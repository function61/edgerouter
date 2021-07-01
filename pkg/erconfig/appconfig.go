// Application configuration data structures
package erconfig

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// can be used to fetch the current state of configuration - the apps Edgerouter knows *right now*,
// based on all the discovery mechanisms used
type CurrentConfigAccessor interface {
	Apps() []Application
	LastUpdated() time.Time
}

type FrontendKind string

const (
	FrontendKindHostname       FrontendKind = "hostname"
	FrontendKindHostnameRegexp FrontendKind = "hostname_regexp"
)

// https://docs.traefik.io/v1.7/basics/#matchers
type Frontend struct {
	Kind            FrontendKind `json:"kind"`
	Hostname        string       `json:"hostname,omitempty"`
	HostnameRegexp  string       `json:"hostname_regexp,omitempty"`
	PathPrefix      string       `json:"path_prefix"` // applies with both kinds
	StripPathPrefix bool         `json:"strip_path_prefix,omitempty"`
}

func (f *Frontend) Validate() error {
	switch f.Kind {
	case FrontendKindHostname:
		if f.Hostname == "" {
			return emptyFieldErr("Hostname")
		}
	case FrontendKindHostnameRegexp:
		if f.HostnameRegexp == "" {
			return emptyFieldErr("HostnameRegexp")
		}

		_, err := regexp.Compile(f.HostnameRegexp)
		if err != nil {
			return fmt.Errorf("HostnameRegexp: %v", err)
		}
	default:
		return fmt.Errorf("unknown frontend kind: %s", f.Kind)
	}

	return nil
}

type Application struct {
	Id        string     `json:"id"` // ACLs can reference this, so keep stable (i.e. service replicas/restarts should not affect this)
	Frontends []Frontend `json:"frontends"`
	Backend   Backend    `json:"backend"`
}

func (a *Application) Validate() error {
	if a.Id == "" {
		return emptyFieldErr("Id")
	}

	if len(a.Frontends) == 0 {
		return emptyFieldErr("Frontends")
	}

	for _, frontend := range a.Frontends {
		if err := frontend.Validate(); err != nil {
			return fmt.Errorf("app %s frontend: %v", a.Id, err)
		}
	}

	switch a.Backend.Kind {
	case BackendKindS3StaticWebsite:
		return a.Backend.S3StaticWebsiteOpts.Validate()
	case BackendKindReverseProxy:
		return a.Backend.ReverseProxyOpts.Validate()
	case BackendKindAwsLambda:
		return a.Backend.AwsLambdaOpts.Validate()
	case BackendKindEdgerouterAdmin:
		return nil
	case BackendKindAuthV0:
		return a.Backend.AuthV0Opts.Validate()
	case BackendKindAuthSso:
		return a.Backend.AuthSsoOpts.Validate()
	case BackendKindRedirect:
		return a.Backend.RedirectOpts.Validate()
	default:
		return fmt.Errorf("app %s backend unkown kind: %s", a.Id, a.Backend.Kind)
	}
}

// when adding new kind, remember to update:
// - Application.Validate()
// - Backend.Describe()
// - factory in backendfactory
type BackendKind string

const (
	BackendKindS3StaticWebsite BackendKind = "s3_static_website"
	BackendKindReverseProxy    BackendKind = "reverse_proxy"
	BackendKindAwsLambda       BackendKind = "aws_lambda"
	BackendKindEdgerouterAdmin BackendKind = "edgerouter_admin"
	BackendKindAuthV0          BackendKind = "auth_v0"
	BackendKindAuthSso         BackendKind = "auth_sso"
	BackendKindRedirect        BackendKind = "redirect"
)

type Backend struct {
	Kind                BackendKind                 `json:"kind"`
	S3StaticWebsiteOpts *BackendOptsS3StaticWebsite `json:"s3_static_website_opts,omitempty"`
	ReverseProxyOpts    *BackendOptsReverseProxy    `json:"reverse_proxy_opts,omitempty"`
	AwsLambdaOpts       *BackendOptsAwsLambda       `json:"aws_lambda_opts,omitempty"`
	AuthV0Opts          *BackendOptsAuthV0          `json:"auth_v0_opts,omitempty"`
	AuthSsoOpts         *BackendOptsAuthSso         `json:"auth_sso_opts,omitempty"`
	RedirectOpts        *BackendOptsRedirect        `json:"redirect_opts,omitempty"`
}

type BackendOptsS3StaticWebsite struct {
	BucketName      string `json:"bucket_name"`
	RegionId        string `json:"region_id"`
	DeployedVersion string `json:"deployed_version"`   // can be empty before first deployed version
	NotFoundPage    string `json:"404_page,omitempty"` // (optional) ex: "404.html", relative to root of deployed site
}

func (b *BackendOptsS3StaticWebsite) Validate() error {
	if b.BucketName == "" {
		return emptyFieldErr("BucketName")
	}

	if b.RegionId == "" {
		return emptyFieldErr("RegionId")
	}

	return nil
}

type BackendOptsReverseProxy struct {
	Origins           []string          `json:"origins"`
	TlsConfig         *TlsConfig        `json:"tls_config,omitempty"`
	Caching           bool              `json:"caching,omitempty"`             // turn on response caching?
	PassHostHeader    bool              `json:"pass_host_header,omitempty"`    // use client-sent Host (=true) or origin's hostname? (=false) https://doc.traefik.io/traefik/routing/services/#pass-host-header
	IndexDocument     string            `json:"index_document,omitempty"`      // if request path ends in /foo/ ("directory"), rewrite it into /foo/index.html
	RemoveQueryString bool              `json:"remove_query_string,omitempty"` // reduces cache misses if responses don't vary on qs
	HeadersToOrigin   map[string]string `json:"headers_to_origin,omitempty"`   // force-add headers to be sent to origin
}

func (b *BackendOptsReverseProxy) Validate() error {
	if len(b.Origins) == 0 {
		return emptyFieldErr("Origins")
	}

	return nil
}

type BackendOptsAwsLambda struct {
	FunctionName string `json:"function_name"`
	RegionId     string `json:"region_id"`
}

func (b *BackendOptsAwsLambda) Validate() error {
	if b.FunctionName == "" {
		return emptyFieldErr("FunctionName")
	}

	if b.RegionId == "" {
		return emptyFieldErr("RegionId")
	}

	return nil
}

type BackendOptsAuthV0 struct {
	BearerToken       string   `json:"bearer_token"`
	AuthorizedBackend *Backend `json:"authorized_backend"` // ptr for validation
}

func (b *BackendOptsAuthV0) Validate() error {
	if b.BearerToken == "" {
		return emptyFieldErr("BearerToken")
	}

	if b.AuthorizedBackend == nil {
		return emptyFieldErr("AuthorizedBackend")
	}

	return nil
}

type BackendOptsAuthSso struct {
	IdServerUrl       string   `json:"id_server_url,omitempty"`
	AllowedUserIds    []string `json:"allowed_user_ids"`
	Audience          string   `json:"audience"`
	AuthorizedBackend *Backend `json:"authorized_backend"` // ptr for validation
}

func (b *BackendOptsAuthSso) Validate() error {
	if b.AuthorizedBackend == nil {
		return emptyFieldErr("AuthorizedBackend")
	}

	// temporarily disabled due to prod conf
	if b.Audience == "" {
		return emptyFieldErr("Audience")
	}

	return nil
}

type BackendOptsRedirect struct {
	To string `json:"to"`
}

func (b *BackendOptsRedirect) Validate() error {
	if b.To == "" {
		return emptyFieldErr("To")
	}

	return nil
}

// factories

func SimpleApplication(id string, frontend Frontend, backend Backend) Application {
	return Application{
		Id: id,
		Frontends: []Frontend{
			frontend,
		},
		Backend: backend,
	}
}

func SimpleHostnameFrontend(hostname string, options ...FrontendOpt) Frontend {
	opts := getFrontendOptions(options)

	return Frontend{
		Kind:            FrontendKindHostname,
		Hostname:        hostname,
		PathPrefix:      opts.pathPrefix,
		StripPathPrefix: opts.stripPathPrefix,
	}
}

func RegexpHostnameFrontend(hostnameRegexp string, options ...FrontendOpt) Frontend {
	opts := getFrontendOptions(options)

	return Frontend{
		Kind:            FrontendKindHostnameRegexp,
		HostnameRegexp:  hostnameRegexp,
		PathPrefix:      opts.pathPrefix,
		StripPathPrefix: opts.stripPathPrefix,
	}
}

// catches all requests (essentially accepting all hostnames)
func CatchAllHostnamesFrontend(options ...FrontendOpt) Frontend {
	return RegexpHostnameFrontend("{.+}", options...)
}

func S3Backend(bucketName string, regionId string, deployedVersion string) Backend {
	return Backend{
		Kind: BackendKindS3StaticWebsite,
		S3StaticWebsiteOpts: &BackendOptsS3StaticWebsite{
			BucketName:      bucketName,
			RegionId:        regionId,
			DeployedVersion: deployedVersion,
		},
	}
}

func ReverseProxyBackend(addrs []string, tlsConfig *TlsConfig, passHostHeader bool) Backend {
	return Backend{
		Kind: BackendKindReverseProxy,
		ReverseProxyOpts: &BackendOptsReverseProxy{
			Origins:        addrs,
			TlsConfig:      tlsConfig,
			PassHostHeader: passHostHeader,
		},
	}
}

func RedirectBackend(to string) Backend {
	return Backend{
		Kind: BackendKindRedirect,
		RedirectOpts: &BackendOptsRedirect{
			To: to,
		},
	}
}

func LambdaBackend(functionName string, regionId string) Backend {
	return Backend{
		Kind: BackendKindAwsLambda,
		AwsLambdaOpts: &BackendOptsAwsLambda{
			FunctionName: functionName,
			RegionId:     regionId,
		},
	}
}

func EdgerouterAdminBackend() Backend {
	return Backend{
		Kind: BackendKindEdgerouterAdmin,
	}
}

func AuthV0Backend(bearerToken string, authorizedBackend Backend) Backend {
	return Backend{
		Kind: BackendKindAuthV0,
		AuthV0Opts: &BackendOptsAuthV0{
			BearerToken:       bearerToken,
			AuthorizedBackend: &authorizedBackend,
		},
	}
}

func AuthSsoBackend(
	idServerUrl string,
	allowedUserIds []string,
	audience string,
	authorizedBackend Backend,
) Backend {
	return Backend{
		Kind: BackendKindAuthSso,
		AuthSsoOpts: &BackendOptsAuthSso{
			IdServerUrl:       idServerUrl,
			AllowedUserIds:    allowedUserIds,
			Audience:          audience,
			AuthorizedBackend: &authorizedBackend,
		},
	}
}

// describers

func (a *Application) Describe() string {
	lines := []string{
		a.Id,
		"  backend = " + a.Backend.Describe(),
	}

	for _, frontend := range a.Frontends {
		lines = append(lines, "  frontend = "+frontend.Describe())
	}

	return strings.Join(lines, "\n")
}

func (f *Frontend) Describe() string {
	switch f.Kind {
	case FrontendKindHostname:
		return string(f.Kind) + ":" + f.Hostname + f.PathPrefix
	case FrontendKindHostnameRegexp:
		return string(f.Kind) + ":" + f.HostnameRegexp + f.PathPrefix
	default:
		return string(f.Kind)
	}
}

func (b *Backend) Describe() string {
	switch b.Kind {
	case BackendKindS3StaticWebsite:
		return string(b.Kind) + ":" + b.S3StaticWebsiteOpts.DeployedVersion
	case BackendKindReverseProxy:
		return string(b.Kind) + ":" + strings.Join(b.ReverseProxyOpts.Origins, ", ")
	case BackendKindAwsLambda:
		return string(b.Kind) + ":" + fmt.Sprintf("%s@%s", b.AwsLambdaOpts.FunctionName, b.AwsLambdaOpts.RegionId)
	case BackendKindAuthV0:
		return string(b.Kind) + ":" + fmt.Sprintf("[bearerToken=...] -> %s", b.AuthV0Opts.AuthorizedBackend.Describe())
	case BackendKindRedirect:
		return string(b.Kind) + ":" + b.RedirectOpts.To
	case BackendKindAuthSso:
		return string(b.Kind) + ":" + fmt.Sprintf("[audience=%s] -> %s", b.AuthSsoOpts.Audience, b.AuthSsoOpts.AuthorizedBackend.Describe())
	default:
		return string(b.Kind)
	}
}

type TlsConfig struct {
	InsecureSkipVerify bool   `json:"insecure_skip_verify,omitempty"`
	ServerName         string `json:"server_name,omitempty"` // used to verify the hostname on the server cert. also sent via SNI
}

func (t *TlsConfig) HasMeaningfulContent() bool {
	if t.InsecureSkipVerify || t.ServerName != "" {
		return true
	} else {
		return false
	}
}

func (t *TlsConfig) SelfOrNilIfNoMeaningfulContent() *TlsConfig {
	if t.HasMeaningfulContent() {
		return t
	} else {
		return nil
	}
}

// TODO: gokit/builtin
func emptyFieldErr(fieldName string) error {
	return fmt.Errorf("field %s cannot be empty", fieldName)
}

// frontend options builder
type frontendOptions struct {
	pathPrefix      string
	stripPathPrefix bool
}

func getFrontendOptions(fns []FrontendOpt) frontendOptions {
	opts := &frontendOptions{
		pathPrefix: "/",
	}

	for _, fn := range fns {
		fn(opts)
	}

	return *opts
}

type FrontendOpt func(opts *frontendOptions)

func PathPrefix(pathPrefix string) FrontendOpt {
	return func(opts *frontendOptions) {
		opts.pathPrefix = pathPrefix
	}
}

func StripPathPrefix(opts *frontendOptions) { opts.stripPathPrefix = true }
