// Calls Lambda function with HTTP semantics (impersonates API Gateway)
package lambdabackend

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/function61/edgerouter/pkg/awshelpers"
	"github.com/function61/edgerouter/pkg/erbackend"
	"github.com/function61/edgerouter/pkg/erconfig"
	"io"
	"log"
	"net/http"
	"net/url"
)

type lambdaBackend struct {
	functionName string
	lambda       *lambda.Lambda
}

func New(app erconfig.Application) erbackend.Backend {
	creds, err := awshelpers.GetCredentials()
	if err != nil {
		panic(err)
	}

	manualCredential := credentials.NewStaticCredentials(
		creds.AccessKeyId,
		creds.AccessKeySecret,
		"")

	awsSession, err := session.NewSession()
	if err != nil {
		panic(err)
	}

	lopts := app.Backend.AwsLambdaOpts

	return &lambdaBackend{
		functionName: lopts.FunctionName,
		lambda: lambda.New(
			awsSession,
			aws.NewConfig().WithCredentials(manualCredential).WithRegion(lopts.RegionId)),
	}
}

func (b *lambdaBackend) Serve(w http.ResponseWriter, r *http.Request) {
	log.Printf("requesting path %s", r.URL.String())

	requestBodyBase64 := &bytes.Buffer{}
	if _, err := io.Copy(base64.NewEncoder(base64.StdEncoding, requestBodyBase64), r.Body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	headers, err := copyHeaders(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	e := events.APIGatewayProxyRequest{
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
		e.Body = requestBodyBase64.String()
		e.IsBase64Encoded = true
	}

	payload, err := json.Marshal(&e)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out, err := b.lambda.Invoke(&lambda.InvokeInput{
		FunctionName: aws.String(b.functionName),
		Payload:      payload,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)

		if _, err := w.Write([]byte(err.Error())); err != nil {
			panic(err)
		}
		return
	}

	payloadResponse := &events.APIGatewayProxyResponse{}
	if err := json.Unmarshal(out.Payload, payloadResponse); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	// Lambda had some error which caused it to not spit out APIGatewayProxyResponse?
	if payloadResponse.StatusCode == 0 {
		http.Error(w, "upstream did not provide correct APIGatewayProxyResponse", http.StatusBadGateway)
		return
	}

	if err := proxyApiGatewayResponse(payloadResponse, w); err != nil {
		// TODO: if we've already wrote headers, this might not succeed
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