// Calls Lambda function with HTTP semantics (impersonates API Gateway)
package lambdabackend

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/function61/edgerouter/pkg/erconfig"
	"github.com/function61/gokit/aws/s3facade"
)

type lambdaBackend struct {
	functionName string
	lambda       *lambda.Lambda
}

func New(opts erconfig.BackendOptsAwsLambda) (http.Handler, error) {
	creds, err := s3facade.CredentialsFromEnv()
	if err != nil {
		return nil, err
	}

	awsSession, err := session.NewSession()
	if err != nil {
		return nil, err
	}

	return &lambdaBackend{
		functionName: opts.FunctionName,
		lambda: lambda.New(
			awsSession,
			aws.NewConfig().WithCredentials(creds).WithRegion(opts.RegionId)),
	}, nil
}

func (b *lambdaBackend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Printf("requesting path %s", r.URL.String())

	defer r.Body.Close()

	requestBodyBase64 := &bytes.Buffer{}
	requestBodyBase64Encoder := base64.NewEncoder(base64.StdEncoding, requestBodyBase64)
	if _, err := io.Copy(requestBodyBase64Encoder, r.Body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := requestBodyBase64Encoder.Close(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	headers, err := copyHeaders(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	proxyRequest := events.APIGatewayProxyRequest{
		Resource:              "/",
		Path:                  r.URL.Path,
		Headers:               headers,
		HTTPMethod:            r.Method,
		QueryStringParameters: queryParametersToSimpleMap(r.URL.Query()),
		RequestContext:        events.APIGatewayProxyRequestContext{
			// APIID: "dummy",
		},
	}

	if requestBodyBase64.Len() > 0 {
		proxyRequest.Body = requestBodyBase64.String()
		proxyRequest.IsBase64Encoded = true
	}

	proxyRequestJson, err := json.Marshal(&proxyRequest)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	lambdaResponse, err := b.lambda.Invoke(&lambda.InvokeInput{
		FunctionName: aws.String(b.functionName),
		Payload:      proxyRequestJson,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	payloadResponse := &events.APIGatewayProxyResponse{}
	if err := json.Unmarshal(lambdaResponse.Payload, payloadResponse); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	// Lambda had some error which caused it to not spit out APIGatewayProxyResponse?
	if payloadResponse.StatusCode == 0 {
		http.Error(w, "upstream did not provide correct APIGatewayProxyResponse", http.StatusBadGateway)
		return
	}

	if err := proxyApiGatewayResponse(payloadResponse, w); err != nil {
		// TODO: if we already wrote headers, this will not succeed
		http.Error(w, err.Error(), http.StatusBadGateway)
	}
}

func proxyApiGatewayResponse(payloadResponse *events.APIGatewayProxyResponse, w http.ResponseWriter) error {
	responseHeaders := w.Header()

	for key, val := range payloadResponse.Headers {
		responseHeaders.Set(key, val)
	}

	w.WriteHeader(payloadResponse.StatusCode)

	if len(payloadResponse.Body) == 0 { // our job is done
		return nil
	}

	bodyToWrite := []byte(payloadResponse.Body)

	if payloadResponse.IsBase64Encoded {
		var err error
		bodyToWrite, err = base64.StdEncoding.DecodeString(payloadResponse.Body)
		if err != nil {
			return err
		}
	}

	_, err := w.Write(bodyToWrite)
	return err
}

func queryParametersToSimpleMap(queryPars url.Values) map[string]string {
	params := map[string]string{}
	for key := range queryPars {
		params[key] = queryPars.Get(key)
	}

	return params
}

func copyHeaders(r *http.Request) (map[string]string, error) {
	headers := map[string]string{}

	for key, vals := range r.Header {
		if len(vals) != 1 {
			return nil, fmt.Errorf("multi-valued headers (got key %s) not implemented yet", key)
		}

		headers[key] = vals[0]
	}

	return headers, nil
}
