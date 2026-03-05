// Calls Lambda function with HTTP semantics (impersonates API Gateway)
package lambdabackend

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/function61/edgerouter/pkg/erconfig"
	"github.com/function61/edgerouter/pkg/turbocharger"
)

type lambdaBackend struct {
	functionName string
	lambda       *lambda.Client
	isPayloadV2  bool // https://docs.aws.amazon.com/apigateway/latest/developerguide/http-api-develop-integrations-lambda.html
}

func New(ctx context.Context, opts erconfig.BackendOptsAwsLambda, logger *log.Logger) (http.Handler, error) {
	awsConfig, err := config.LoadDefaultConfig(ctx, config.WithRegion(opts.RegionID))
	if err != nil {
		return nil, err
	}

	isPayloadV2, err := validatePayloadFormatVersion(opts.PayloadFormatVersion)
	if err != nil {
		return nil, err
	}

	handler := &lambdaBackend{
		functionName: opts.FunctionName,
		lambda:       lambda.NewFromConfig(awsConfig),
		isPayloadV2:  isPayloadV2,
	}

	return turbocharger.WrapWithMiddlewareIfConfigAvailable(ctx, handler, logger)
}

func (b *lambdaBackend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if b.isPayloadV2 {
		// https://pkg.go.dev/github.com/aws/aws-lambda-go/events#APIGatewayV2HTTPRequest
		b.serveHTTPModel(w, r)
	} else {
		// https://pkg.go.dev/github.com/aws/aws-lambda-go/events#APIGatewayProxyRequest
		// https://docs.aws.amazon.com/apigateway/latest/developerguide/set-up-lambda-proxy-integrations.html#api-gateway-simple-proxy-for-lambda-input-format
		b.serveRESTModel(w, r)
	}
}

func (b *lambdaBackend) serveHTTPModel(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	bodyBase64, err := encodeToBase64RawStd(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	headers, err := copyHeaders(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	sourceIP, _, _ := net.SplitHostPort(r.RemoteAddr)

	now := time.Now().UTC()

	const routeKey = "$default"

	proxyRequestJSON, err := json.Marshal(events.APIGatewayV2HTTPRequest{
		Version:        "2.0",
		RouteKey:       routeKey,
		RawPath:        r.URL.Path,
		RawQueryString: r.URL.RawQuery,
		// Cookies:               []string{},
		Headers:               headers,
		QueryStringParameters: queryParametersToSimpleMap(r.URL.Query()),
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			RouteKey: routeKey,
			Stage:    "$default",
			// RequestID:  "",
			DomainName: r.URL.Host,
			Time:       now.Format("02/Jan/2006:15:04:05 -0700"), // what a dumbass format
			TimeEpoch:  now.UnixMilli(),
			HTTP: events.APIGatewayV2HTTPRequestContextHTTPDescription{
				Method:    r.Method,
				Path:      r.URL.Path,
				Protocol:  r.Proto,
				SourceIP:  sourceIP,
				UserAgent: r.UserAgent(),
			},
		},
		Body:            bodyBase64,
		IsBase64Encoded: true,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	lambdaResponse, err := b.lambda.Invoke(r.Context(), &lambda.InvokeInput{
		FunctionName: aws.String(b.functionName),
		Payload:      proxyRequestJSON,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	payloadResponse := &events.APIGatewayV2HTTPResponse{}
	if err := json.Unmarshal(lambdaResponse.Payload, payloadResponse); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	if err := proxyAPIGatewayResponse(payloadResponse, w); err != nil {
		// TODO: if we already wrote headers, this will not succeed
		http.Error(w, err.Error(), http.StatusBadGateway)
	}
}

func (b *lambdaBackend) serveRESTModel(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	requestBodyBase64, err := encodeToBase64RawStd(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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

	if len(requestBodyBase64) > 0 {
		proxyRequest.Body = requestBodyBase64
		proxyRequest.IsBase64Encoded = true
	}

	proxyRequestJSON, err := json.Marshal(&proxyRequest)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	lambdaResponse, err := b.lambda.Invoke(r.Context(), &lambda.InvokeInput{
		FunctionName: aws.String(b.functionName),
		Payload:      proxyRequestJSON,
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

	if err := proxyAPIGatewayResponse(&events.APIGatewayV2HTTPResponse{
		StatusCode:        payloadResponse.StatusCode,
		Headers:           payloadResponse.Headers,
		MultiValueHeaders: payloadResponse.MultiValueHeaders,
		Body:              payloadResponse.Body,
		IsBase64Encoded:   payloadResponse.IsBase64Encoded,
		// only field missing from old struct: `Cookies`
	}, w); err != nil {
		// TODO: if we already wrote headers, this will not succeed
		http.Error(w, err.Error(), http.StatusBadGateway)
	}
}

func proxyAPIGatewayResponse(payloadResponse *events.APIGatewayV2HTTPResponse, w http.ResponseWriter) error {
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

func encodeToBase64RawStd(content io.Reader) (string, error) {
	bodyBuffered := &bytes.Buffer{}
	encodeBase64 := base64.NewEncoder(base64.RawStdEncoding, bodyBuffered)
	if _, err := io.Copy(encodeBase64, content); err != nil {
		return "", err
	}
	if err := encodeBase64.Close(); err != nil {
		return "", err
	}
	return bodyBuffered.String(), nil
}

func validatePayloadFormatVersion(version string) (bool, error) {
	switch version {
	case "1.0", "":
		return false, nil
	case "2.0":
		return true, nil
	default:
		return false, fmt.Errorf("unrecognized PayloadFormatVersion: %s", version)
	}
}
