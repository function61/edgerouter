package erconfig

import (
	"fmt"
	"regexp"
	"strings"
)

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
	Id        string     `json:"id"`
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
	case BackendKindPeerSet:
		return a.Backend.PeerSetOpts.Validate()
	case BackendKindAwsLambda:
		return a.Backend.AwsLambdaOpts.Validate()
	case BackendKindEdgerouterAdmin:
		return nil
	default:
		return fmt.Errorf("app %s backend unkown kind: %s", a.Id, a.Backend.Kind)
	}
}

type BackendKind string

const (
	BackendKindS3StaticWebsite BackendKind = "s3_static_website"
	BackendKindPeerSet         BackendKind = "peer_set"
	BackendKindAwsLambda       BackendKind = "aws_lambda"
	BackendKindEdgerouterAdmin BackendKind = "edgerouter_admin"
)

type Backend struct {
	Kind                BackendKind                 `json:"kind"`
	S3StaticWebsiteOpts *BackendOptsS3StaticWebsite `json:"s3_static_website_opts,omitempty"`
	PeerSetOpts         *BackendOptsPeerSet         `json:"peer_set_opts,omitempty"`
	AwsLambdaOpts       *BackendOptsAwsLambda       `json:"aws_lambda_opts,omitempty"`
}

type BackendOptsS3StaticWebsite struct {
	BucketName      string `json:"bucket_name"`
	RegionId        string `json:"region_id"`
	DeployedVersion string `json:"deployed_version"` // can be empty before first deployed version
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

type BackendOptsPeerSet struct {
	Addrs     []string   `json:"addrs"`
	TlsConfig *TlsConfig `json:"tls_config"`
}

func (b *BackendOptsPeerSet) Validate() error {
	if len(b.Addrs) == 0 {
		return emptyFieldErr("Addrs")
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

func SimpleHostnameFrontend(hostname string, pathPrefix string, stripPath bool) Frontend {
	return Frontend{
		Kind:            FrontendKindHostname,
		Hostname:        hostname,
		PathPrefix:      pathPrefix,
		StripPathPrefix: stripPath,
	}
}

func RegexpHostnameFrontend(hostnameRegexp string, pathPrefix string) Frontend {
	return Frontend{
		Kind:           FrontendKindHostnameRegexp,
		HostnameRegexp: hostnameRegexp,
		PathPrefix:     pathPrefix,
	}
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

func PeerSetBackend(addrs []string, tlsConfig *TlsConfig) Backend {
	return Backend{
		Kind: BackendKindPeerSet,
		PeerSetOpts: &BackendOptsPeerSet{
			Addrs:     addrs,
			TlsConfig: tlsConfig,
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
	case BackendKindPeerSet:
		return string(b.Kind) + ":" + strings.Join(b.PeerSetOpts.Addrs, ", ")
	case BackendKindAwsLambda:
		return string(b.Kind) + ":" + fmt.Sprintf("%s@%s", b.AwsLambdaOpts.FunctionName, b.AwsLambdaOpts.RegionId)
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

func emptyFieldErr(fieldName string) error {
	return fmt.Errorf("field %s cannot be empty", fieldName)
}