// Discovers applications from Docker Swarm cluster
package swarmdiscovery

import (
	"context"
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

func (s *swarmDiscovery) ReadApplications(ctx context.Context) ([]erconfig.Application, error) {
	swarmServices, err := discoverSwarmServices(ctx, s.dockerUrl, s.dockerNetworkName, s.dockerClient)
	if err != nil {
		return nil, err
	}

	bareContainers, err := discoverDockerContainers(ctx, s.dockerUrl, s.dockerNetworkName, s.dockerClient)
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

func discoverDockerContainers(
	ctx context.Context,
	dockerUrl string,
	dockerNetworkName string,
	dockerClient *http.Client,
) ([]Service, error) {
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
		if len(container.Names) == 0 {
			continue
		}

		ipAddress := ""
		if settings, found := container.NetworkSettings.Networks[dockerNetworkName]; found {
			ipAddress = settings.IPAddress // prefer IP from the asked dockerNetworkName
		}

		if settings, found := container.NetworkSettings.Networks["bridge"]; ipAddress == "" && found {
			ipAddress = settings.IPAddress // fall back to bridge IP if not found
		}

		if ipAddress == "" {
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
					IPv4:         ipAddress,
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
