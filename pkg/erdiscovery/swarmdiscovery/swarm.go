// Discovers applications from Docker Swarm cluster
package swarmdiscovery

import (
	"context"
	"errors"
	"fmt"
	"github.com/function61/edgerouter/pkg/erconfig"
	"github.com/function61/edgerouter/pkg/erdiscovery"
	"github.com/function61/gokit/envvar"
	"github.com/function61/gokit/ezhttp"
	"github.com/function61/gokit/udocker"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
)

type Service struct {
	Name      string
	Image     string
	Labels    map[string]string
	ENVs      map[string]string
	Instances []ServiceInstance
}

type ServiceInstance struct {
	DockerTaskId string
	NodeID       string
	NodeHostname string
	IPv4         string
}

func HasConfigInEnv() bool {
	return os.Getenv("DOCKER_URL") != ""
}

func New() (erdiscovery.Reader, error) {
	dockerUrl, err := envvar.Required("DOCKER_URL")
	if err != nil {
		return nil, err
	}

	dockerNetworkName, err := envvar.Required("NETWORK_NAME")
	if err != nil {
		return nil, err
	}

	dockerClient, dockerUrlTransformed, err := udocker.Client(
		dockerUrl,
		udocker.ClientCertificateFromEnv,
		true)
	if err != nil {
		return nil, err
	}

	// for unix sockets we need to fake "http://localhost"
	dockerUrl = dockerUrlTransformed

	return &swarmDiscovery{
		dockerNetworkName: dockerNetworkName,
		dockerUrl:         dockerUrl,
		dockerClient:      dockerClient,
	}, nil
}

type swarmDiscovery struct {
	dockerNetworkName string
	dockerUrl         string
	dockerClient      *http.Client
}

// find annotations from here:
//     https://docs.traefik.io/v1.7/configuration/backends/docker/
func traefikAnnotationsToApp(service Service) (*erconfig.Application, error) {
	// require explicit enable flag Traefik
	if service.Labels["traefik.enable"] != "true" {
		return nil, nil
	}

	scheme := "http"
	if proto, has := service.Labels["traefik.protocol"]; has {
		if proto != "http" && proto != "https" {
			return nil, fmt.Errorf("unsupported protocol: %s", proto)
		}

		scheme = proto
	}

	insecureSkipVerify := false

	// doesn't actually seem to exist in Traefik:
	//     https://github.com/containous/traefik/issues/2367
	if insecureSkipVerifyString, has := service.Labels["traefik.backend.tls.insecureSkipVerify"]; has {
		if insecureSkipVerifyString != "true" {
			return nil, fmt.Errorf("unsupported value for insecureSkipVerify: %s", insecureSkipVerifyString)
		}

		if scheme != "https" {
			return nil, errors.New("insecureSkipVerify specified but not using https")
		}

		insecureSkipVerify = true
	}

	// also doesn't exist in Traefik
	tlsServerName := service.Labels["traefik.backend.tls.serverName"]

	port := service.Labels["traefik.port"]
	if port == "" {
		if scheme == "http" {
			port = "80"
		} else if scheme == "https" {
			port = "443"
		}
	}

	frontendRule := service.Labels["traefik.frontend.rule"]
	if frontendRule == "" {
		return nil, fmt.Errorf("skipping traefik.enable'd service without a frontend rule")
	}

	frontend, err := func() (erconfig.Frontend, error) {
		switch {
		case strings.HasPrefix(frontendRule, "Host:"):
			return erconfig.SimpleHostnameFrontend(frontendRule[len("Host:"):], "/", false), nil
		case strings.HasPrefix(frontendRule, "HostRegexp:"):
			return erconfig.RegexpHostnameFrontend(frontendRule[len("HostRegexp:"):], "/"), nil
		default:
			return erconfig.Frontend{}, fmt.Errorf("unsupported frontend rule: %s", frontendRule)
		}
	}()
	if err != nil {
		return nil, err
	}

	addrs := []string{}

	for _, instance := range service.Instances {
		addrs = append(addrs, scheme+"://"+instance.IPv4+":"+port)
	}

	if len(addrs) == 0 {
		return nil, nil
	}

	tlsConfig := &erconfig.TlsConfig{
		InsecureSkipVerify: insecureSkipVerify,
		ServerName:         tlsServerName,
	}

	app := erconfig.SimpleApplication(
		service.Name,
		frontend,
		erconfig.PeerSetBackend(addrs, tlsConfig.SelfOrNilIfNoMeaningfulContent()))

	return &app, nil
}

func (s *swarmDiscovery) ReadApplications(ctx context.Context) ([]erconfig.Application, error) {
	swarmServices, err := discoverSwarmServices(ctx, s.dockerUrl, s.dockerNetworkName, s.dockerClient)
	if err != nil {
		return nil, err
	}

	bareContainers, err := discoverDockerContainers(ctx, s.dockerUrl, s.dockerClient)
	if err != nil {
		return nil, err
	}

	swarmServicesAndBareContainers := append(swarmServices, bareContainers...)

	apps := []erconfig.Application{}

	for _, service := range swarmServicesAndBareContainers {
		app, err := traefikAnnotationsToApp(service)
		if err != nil {
			log.Println(err.Error())
			continue
		}
		if app == nil { // non-error skip
			continue
		}

		apps = append(apps, *app)
	}

	return apps, nil
}

func discoverSwarmServices(ctx context.Context, dockerUrl string, networkName string, dockerClient *http.Client) ([]Service, error) {
	services := []Service{}

	dockerTasks := []udocker.Task{}
	if _, err := ezhttp.Get(
		ctx,
		dockerUrl+udocker.TasksEndpoint,
		ezhttp.Client(dockerClient),
		ezhttp.RespondsJson(&dockerTasks, true),
	); err != nil {
		return nil, err
	}

	dockerServices := []udocker.Service{}
	if _, err := ezhttp.Get(
		ctx,
		dockerUrl+udocker.ServicesEndpoint,
		ezhttp.Client(dockerClient),
		ezhttp.RespondsJson(&dockerServices, true),
	); err != nil {
		return nil, err
	}

	dockerNodes := []udocker.Node{}
	if _, err := ezhttp.Get(
		ctx,
		dockerUrl+udocker.NodesEndpoint,
		ezhttp.Client(dockerClient),
		ezhttp.RespondsJson(&dockerNodes, true),
	); err != nil {
		return nil, err
	}

	for _, dockerService := range dockerServices {
		instances := []ServiceInstance{}

		for _, task := range dockerTasks {
			if task.ServiceID != dockerService.ID {
				continue
			}

			var firstIp net.IP = nil
			attachment := networkAttachmentForNetworkName(task, networkName)
			if attachment != nil {
				// for some reason Docker insists on stuffing the CIDR after the IP
				var err error
				firstIp, _, err = net.ParseCIDR(attachment.Addresses[0])
				if err != nil {
					return nil, err
				}
			}

			if firstIp == nil {
				continue
			}

			// task is not allocated to run on an explicit node yet, skip it since
			// our context is discovering running containers.
			if task.NodeID == "" {
				continue
			}

			node := nodeById(task.NodeID, dockerNodes)
			if node == nil {
				return nil, fmt.Errorf("node %s not found for task %s", task.NodeID, task.ID)
			}

			instances = append(instances, ServiceInstance{
				DockerTaskId: task.ID,
				NodeID:       node.ID,
				NodeHostname: node.Description.Hostname,
				IPv4:         firstIp.String(),
			})
		}

		envs := map[string]string{}

		for _, envSerialized := range dockerService.Spec.TaskTemplate.ContainerSpec.Env {
			envKey, envVal := envvar.Parse(envSerialized)
			if envKey != "" {
				envs[envKey] = envVal
			}
		}

		services = append(services, Service{
			Name:      dockerService.Spec.Name,
			Image:     dockerService.Spec.TaskTemplate.ContainerSpec.Image,
			Labels:    dockerService.Spec.Labels,
			ENVs:      envs,
			Instances: instances,
		})
	}

	return services, nil
}

func discoverDockerContainers(ctx context.Context, dockerUrl string, dockerClient *http.Client) ([]Service, error) {
	services := []Service{}

	containers := []udocker.ContainerListItem{}
	if _, err := ezhttp.Get(
		ctx,
		dockerUrl+udocker.ListContainersEndpoint,
		ezhttp.Client(dockerClient),
		ezhttp.RespondsJson(&containers, true),
	); err != nil {
		return nil, err
	}

	for _, container := range containers {
		bridgeSettings, hasBridge := container.NetworkSettings.Networks["bridge"]
		if !hasBridge || len(container.Names) == 0 {
			continue
		}

		services = append(services, Service{
			Name:   container.Names[0],
			Image:  container.Image,
			Labels: container.Labels,
			ENVs:   map[string]string{},
			Instances: []ServiceInstance{
				{
					DockerTaskId: container.Id,
					NodeID:       "dummy",
					NodeHostname: "dummy",
					IPv4:         bridgeSettings.IPAddress,
				},
			},
		})
	}

	return services, nil
}

func networkAttachmentForNetworkName(task udocker.Task, networkName string) *udocker.TaskNetworkAttachment {
	for _, attachment := range task.NetworksAttachments {
		if attachment.Network.Spec.Name == networkName {
			return &attachment
		}
	}

	return nil
}

func nodeById(id string, nodes []udocker.Node) *udocker.Node {
	for _, node := range nodes {
		if node.ID == id {
			return &node
		}
	}

	return nil
}
