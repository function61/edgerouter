// Discovers applications from Docker and/or Docker Swarm cluster
package dockerdiscovery

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"

	"github.com/function61/edgerouter/pkg/erconfig"
	"github.com/function61/edgerouter/pkg/erdiscovery"
	"github.com/function61/gokit/envvar"
	"github.com/function61/gokit/ezhttp"
	"github.com/function61/gokit/udocker"
)

type Service struct {
	Name      string // container name for bare containers, service name for Swarm/compose services
	Image     string
	Labels    map[string]string
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

	dockerNetworkName := func() string {
		if netName := os.Getenv("NETWORK_NAME"); netName != "" {
			return netName
		}

		// TODO: log warning about missing NETWORK_NAME

		// default to the bridge. this works in non-Swarm contexts and non-overlay networks
		return "bridge"
	}()

	dockerClient, dockerUrlTransformed, err := udocker.Client(
		dockerUrl,
		udocker.ClientCertificateFromEnv,
		true)
	if err != nil {
		return nil, err
	}

	// for unix sockets we need to fake "http://localhost"
	dockerUrl = dockerUrlTransformed

	return &dockerDiscovery{
		dockerNetworkName: dockerNetworkName,
		dockerUrl:         dockerUrl,
		dockerClient:      dockerClient,
	}, nil
}

type dockerDiscovery struct {
	dockerNetworkName string
	dockerUrl         string
	dockerClient      *http.Client
}

func (s *dockerDiscovery) ReadApplications(ctx context.Context) ([]erconfig.Application, error) {
	swarmServices, err := discoverSwarmServices(ctx, s.dockerUrl, s.dockerNetworkName, s.dockerClient)
	if err != nil {
		return nil, err
	}

	bareContainers, err := discoverDockerContainers(ctx, s.dockerUrl, s.dockerNetworkName, s.dockerClient, swarmServices)
	if err != nil {
		return nil, err
	}

	swarmServicesAndBareContainers := []Service{}
	swarmServicesAndBareContainers = append(swarmServicesAndBareContainers, swarmServices...)
	swarmServicesAndBareContainers = append(swarmServicesAndBareContainers, bareContainers...)

	apps := []erconfig.Application{}

	for _, service := range swarmServicesAndBareContainers {
		app, err := traefikAnnotationsToApp(service)
		if err != nil {
			log.Println(fmt.Errorf("%s: traefikAnnotationsToApp: %w", service.Name, err).Error())
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

		// instances now contains the IP endpoints we know for the service (for *NETWORK_NAME*)

		// no reason to "advertise" a service without any instances, especially because we won't try
		// container-based discovery for services we return from here (we might still find IPs for
		// those even if we fail here)
		if len(instances) > 0 {
			services = append(services, Service{
				Name:      dockerService.Spec.Name,
				Image:     dockerService.Spec.TaskTemplate.ContainerSpec.Image,
				Labels:    dockerService.Spec.Labels,
				Instances: instances,
			})
		}
	}

	return services, nil
}

// bare containers that are not necessarily a result of a Swarm service
func discoverDockerContainers(
	ctx context.Context,
	dockerUrl string,
	dockerNetworkName string,
	dockerClient *http.Client,
	alreadyDiscoveredFromSwarm []Service,
) ([]Service, error) {
	services := []Service{}

	// once (for lifetime of this function) = for caching and lazy evaluation because most times
	// this is not needed
	var gwbridgeNetworkInspectOnceCached *DockerNetworkInspectOutput
	gwbridgeNetworkInspectOnce := func() (*DockerNetworkInspectOutput, error) {
		if dockerNetworkName != "docker_gwbridge" { // not asking for docker_gwbridge
			return nil, nil
		}

		if gwbridgeNetworkInspectOnceCached == nil {
			var err error
			gwbridgeNetworkInspectOnceCached, err = networkInspect(ctx, dockerNetworkName, dockerUrl, dockerClient)
			if err != nil {
				gwbridgeNetworkInspectOnceCached = nil // ensure nil on error
				return nil, err
			}
		}

		return gwbridgeNetworkInspectOnceCached, nil
	}

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
		// I don't know if this ever happens
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

		// if container is attached to e.g. an overlay network, but Edgerouter sits e.g. in the host
		// network namespace (= no direct connectivity to the overlay network), our last-ditch effort
		// is to resolve its docker_gwbridge IP, but it is not visible from "$ docker inspect" output,
		// but from "$ docker network inspect docker_gwbridge" instead
		if ipAddress == "" {
			gwbridgeNetworkInspectOutput, err := gwbridgeNetworkInspectOnce()
			if err != nil {
				return nil, err
			}

			if gwbridgeNetworkInspectOutput != nil { // nil = not using gwbridge
				if networkDetails, found := gwbridgeNetworkInspectOutput.Containers[container.Id]; found {
					// of course IP field needs subnet mask embedded in it ..
					if ipWithoutCidr, _, err := net.ParseCIDR(networkDetails.IPv4Address); err == nil {
						ipAddress = ipWithoutCidr.String()
					}
				}
			}
		}

		if ipAddress == "" {
			continue
		}

		// use swarm service name if defined, so we get stable names ("baikal_baikal") instead of
		// "/baikal_baikal.1.mifsjkoi93gwh9yg89c51va0t" for Swarm-based containers. normally we don't
		// use this discoverDockerContainers() but Swarm, but if we use docker_gwbridge this is how
		// we discover conainers outside of Swarm network contexts
		serviceName := coalesce(
			container.Labels["com.docker.swarm.service.name"],
			container.Names[0])

		// if already found from Swarm catalogue, don't add from bare container discovery
		// (so we don't end up with duplicates)
		if svcsContains(alreadyDiscoveredFromSwarm, serviceName) {
			continue
		}

		services = append(services, Service{
			Name:   serviceName,
			Image:  container.Image,
			Labels: container.Labels,
			Instances: []ServiceInstance{
				{
					DockerTaskId: container.Id,
					NodeID:       "dummy", // not applicable in bare container context
					NodeHostname: "dummy",
					IPv4:         ipAddress,
				},
			},
		})
	}

	return services, nil
}

func networkInspect(
	ctx context.Context,
	dockerNetworkName string,
	dockerUrl string,
	dockerClient *http.Client,
) (*DockerNetworkInspectOutput, error) {
	output := &DockerNetworkInspectOutput{}

	_, err := ezhttp.Get(
		ctx, dockerUrl+networkInspectEndpoint(dockerNetworkName),
		ezhttp.Client(dockerClient),
		ezhttp.RespondsJson(output, true))

	return output, err
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

// TODO: these should be in gokit

type DockerNetworkInspectOutput struct {
	Containers map[string]*struct {
		IPv4Address string `json:"IPv4Address"` // looks like 10.0.1.7/24
	} `json:"Containers"`
}

func networkInspectEndpoint(networkId string) string {
	return fmt.Sprintf("/v1.24/networks/%s", networkId)
}

func coalesce(items ...string) string {
	for _, item := range items {
		if item != "" {
			return item
		}
	}

	return ""
}

func svcsContains(services []Service, name string) bool {
	for _, svc := range services {
		if svc.Name == name {
			return true
		}
	}

	return false
}
