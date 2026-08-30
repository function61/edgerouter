// Discovers applications from Docker and/or Docker Swarm cluster
package dockerdiscovery

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"slices"

	"github.com/function61/edgerouter/pkg/erconfig"
	"github.com/function61/edgerouter/pkg/erdiscovery"
	"github.com/function61/gokit/app/udocker"
	"github.com/function61/gokit/net/http/ezhttp"
	"github.com/function61/gokit/os/osutil"
)

type Service struct {
	Name      string // container name for bare containers, service name for Swarm/compose services
	Image     string
	Labels    map[string]string
	Instances []ServiceInstance
}

type ServiceInstance struct {
	DockerTaskID string
	NodeID       string
	NodeHostname string
	IPv4         string
}

func HasConfigInEnv() bool {
	return os.Getenv("DOCKER_URL") != ""
}

func New(logger *slog.Logger) (erdiscovery.Reader, error) {
	dockerURL, err := osutil.GetenvRequired("DOCKER_URL")
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

	dockerClient, dockerURLTransformed, err := udocker.Client(
		dockerURL,
		udocker.ClientCertificateFromEnv,
		true)
	if err != nil {
		return nil, err
	}

	// for unix sockets we need to fake "http://localhost"
	dockerURL = dockerURLTransformed

	return &dockerDiscovery{
		dockerNetworkName: dockerNetworkName,
		dockerURL:         dockerURL,
		dockerClient:      dockerClient,
		dockerAPIVersion: func() udocker.VersionedEndpoints {
			if dockerAPIVersion := os.Getenv("DOCKER_API_VERSION"); dockerAPIVersion != "" {
				return udocker.EndpointVersion(dockerAPIVersion)
			} else {
				return udocker.DefaultVersion
			}
		}(),
		logger: logger,
	}, nil
}

type dockerDiscovery struct {
	dockerNetworkName string
	dockerURL         string
	dockerClient      *http.Client
	dockerAPIVersion  udocker.VersionedEndpoints
	logger            *slog.Logger
}

func (s *dockerDiscovery) ReadApplications(ctx context.Context) ([]erconfig.Application, error) {
	swarmServices, err := discoverSwarmServices(ctx, s.dockerURL, s.dockerNetworkName, s.dockerClient, s.dockerAPIVersion)
	if err != nil {
		return nil, err
	}

	bareContainers, err := discoverDockerContainers(ctx, s.dockerURL, s.dockerNetworkName, s.dockerClient, s.dockerAPIVersion, swarmServices, s.logger)
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
			s.logger.Error("traefikAnnotationsToApp",
				"service", service.Name,
				"error", err)
			continue
		}
		if app == nil { // non-error skip
			continue
		}

		apps = append(apps, *app)
	}

	return apps, nil
}

func discoverSwarmServices(ctx context.Context, dockerURL string, networkName string, dockerClient *http.Client, dockerAPIVersion udocker.VersionedEndpoints) ([]Service, error) {
	services := []Service{}

	if os.Getenv("ENABLE_SWARM") != "true" {
		return services, nil
	}

	dockerTasks := []udocker.Task{}
	if _, err := ezhttp.Get(
		ctx,
		dockerURL+dockerAPIVersion.TasksEndpoint(),
		ezhttp.Client(dockerClient),
		ezhttp.RespondsJSONAllowUnknownFields(&dockerTasks),
	); err != nil {
		return nil, err
	}

	dockerServices := []udocker.Service{}
	if _, err := ezhttp.Get(
		ctx,
		dockerURL+dockerAPIVersion.ServicesEndpoint(),
		ezhttp.Client(dockerClient),
		ezhttp.RespondsJSONAllowUnknownFields(&dockerServices),
	); err != nil {
		return nil, err
	}

	dockerNodes := []udocker.Node{}
	if _, err := ezhttp.Get(
		ctx,
		dockerURL+dockerAPIVersion.NodesEndpoint(),
		ezhttp.Client(dockerClient),
		ezhttp.RespondsJSONAllowUnknownFields(&dockerNodes),
	); err != nil {
		return nil, err
	}

	for _, dockerService := range dockerServices {
		instances := []ServiceInstance{}

		for _, task := range dockerTasks {
			if task.ServiceID != dockerService.ID {
				continue
			}

			var firstIP net.IP = nil
			attachment := networkAttachmentForNetworkName(task, networkName)
			if attachment != nil {
				// for some reason Docker insists on stuffing the CIDR after the IP
				var err error
				firstIP, _, err = net.ParseCIDR(attachment.Addresses[0])
				if err != nil {
					return nil, err
				}
			}

			if firstIP == nil {
				continue
			}

			// task is not allocated to run on an explicit node yet, skip it since
			// our context is discovering running containers.
			if task.NodeID == "" {
				continue
			}

			node := nodeByID(task.NodeID, dockerNodes)
			if node == nil {
				return nil, fmt.Errorf("node %s not found for task %s", task.NodeID, task.ID)
			}

			instances = append(instances, ServiceInstance{
				DockerTaskID: task.ID,
				NodeID:       node.ID,
				NodeHostname: node.Description.Hostname,
				IPv4:         firstIP.String(),
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
	dockerURL string,
	dockerNetworkName string,
	dockerClient *http.Client,
	dockerAPIVersion udocker.VersionedEndpoints,
	alreadyDiscoveredFromSwarm []Service,
	logger *slog.Logger,
) ([]Service, error) {
	services := []Service{}

	// once (for lifetime of this function) = for caching and lazy evaluation because most times
	// this is not needed
	var gwbridgeNetworkInspectOnceCached *udocker.NetworkInspectOutput
	gwbridgeNetworkInspectOnce := func() (*udocker.NetworkInspectOutput, error) {
		if dockerNetworkName != "docker_gwbridge" { // not asking for docker_gwbridge
			return nil, nil
		}

		if gwbridgeNetworkInspectOnceCached == nil {
			var err error
			gwbridgeNetworkInspectOnceCached, err = networkInspect(ctx, dockerNetworkName, dockerURL, dockerClient, dockerAPIVersion)
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
		dockerURL+dockerAPIVersion.ListContainersEndpoint(),
		ezhttp.Client(dockerClient),
		ezhttp.RespondsJSONAllowUnknownFields(&containers),
	); err != nil {
		return nil, err
	}

	for _, container := range containers {
		// I don't know if this ever happens
		if len(container.Names) == 0 {
			continue
		}

		ipAddress := ""
		ipFound := func() bool { return ipAddress != "" }

		if !ipFound() && dockerNetworkName == "_auto_" { // automatic resolving by trusting the reported IP address
			ipAddress = func() string {
				if len(container.NetworkSettings.Networks) != 1 {
					return ""
				}

				for _, nwk := range container.NetworkSettings.Networks { // only 1 loop iteration
					return nwk.IPAddress
				}

				return ""
			}()
		}

		if settings, found := container.NetworkSettings.Networks[dockerNetworkName]; !ipFound() && found {
			ipAddress = settings.IPAddress // prefer IP from the asked dockerNetworkName
		}

		if settings, found := container.NetworkSettings.Networks["bridge"]; !ipFound() && found {
			ipAddress = settings.IPAddress // fall back to bridge IP if not found
		}

		if settings, found := container.NetworkSettings.Networks["host"]; !ipFound() && found {
			// when host network, settings doesn't specify IP address
			if settings.IPAddress != "" {
				logger.Warn("IPAddress not expected for host", "container", container.Id)
				continue
			}
			ipAddress = "127.0.0.1"
		}

		// if container is attached to e.g. an overlay network, but Edgerouter sits e.g. in the host
		// network namespace (= no direct connectivity to the overlay network), our last-ditch effort
		// is to resolve its docker_gwbridge IP, but it is not visible from "$ docker inspect" output,
		// but from "$ docker network inspect docker_gwbridge" instead
		if !ipFound() {
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

		if !ipFound() {
			continue
		}

		// service name is kind of important as it might be used in different kinds of identifiers.
		// maybe in ACLs, to query logs etc.
		//
		// let's try to use the most stable thing. the container name is not great, as Compose laid the names like:
		// - /calendarserver_baikal_1
		// then they arbitrarily changed it from underscores to dashes:
		// - /calendarserver-baikal-1
		//
		// so looks like the compose project name is more stable and reliable, only after that prefer the container name.
		//
		// use swarm service name if defined, so we get stable names ("baikal_baikal") instead of
		// "/baikal_baikal.1.mifsjkoi93gwh9yg89c51va0t" for Swarm-based containers. normally we don't
		// use this discoverDockerContainers() for Swarm, but if we use docker_gwbridge this is how
		// we discover conainers outside of Swarm network contexts
		serviceName := cmp.Or(
			container.Labels["com.docker.compose.project"],
			container.Labels["com.docker.swarm.service.name"],
			container.Names[0])

		// if already found from Swarm catalogue, don't add from bare container discovery
		// (so we don't end up with duplicates)
		if slices.ContainsFunc(alreadyDiscoveredFromSwarm, func(svc Service) bool { return svc.Name == serviceName }) {
			continue
		}

		services = append(services, Service{
			Name:   serviceName,
			Image:  container.Image,
			Labels: container.Labels,
			Instances: []ServiceInstance{
				{
					DockerTaskID: container.Id,
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
	dockerURL string,
	dockerClient *http.Client,
	dockerAPIVersion udocker.VersionedEndpoints,
) (*udocker.NetworkInspectOutput, error) {
	output := &udocker.NetworkInspectOutput{}

	_, err := ezhttp.Get(
		ctx, dockerURL+dockerAPIVersion.NetworkInspectEndpoint(dockerNetworkName),
		ezhttp.Client(dockerClient),
		ezhttp.RespondsJSONAllowUnknownFields(output))

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

func nodeByID(id string, nodes []udocker.Node) *udocker.Node {
	for _, node := range nodes {
		if node.ID == id {
			return &node
		}
	}

	return nil
}
