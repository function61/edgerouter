Installation
============

Contents:

- [Requirements](#requirements)
  * [Platform](#platform)
  * [Docker](#docker)
  * [EventHorizon](#eventhorizon)
- [AWS IAM config](#aws-iam-config)
  * [EventHorizon](#eventhorizon-1)
  * [Lambda functions](#lambda-functions)
  * [S3 static website deployment](#s3-static-website-deployment)
- [Config required pre-start](#config-required-pre-start)
  * [A note about Docker service discovery](#a-note-about-docker-service-discovery)
- [Runtime config](#runtime-config)
- [Start Edgerouter](#start-edgerouter)


Requirements
------------

### Platform

Edgerouter requires Linux amd64. Other platforms could easily be supported but I haven't
had the need so I haven't added cross compilation support yet.


### Docker

Usage via Docker is recommended. It'd be easy to add Systemd unit so you can run it as a
bare binary, but I haven't had a need for it.


### EventHorizon

Edgerouter requires [EventHorizon](https://github.com/function61/eventhorizon). Edgerouter
uses it for:

- CertBus (**required**)
- Service discovery (**optional**)

You must have
gone through its [Installation guide](https://github.com/function61/eventhorizon#installation).


AWS IAM config
--------------

You should have a dedicated user in IAM for Edgerouter's use. A good name could be `edgerouter`.

### EventHorizon

Your Edgerouter user needs to have permissions for EventHorizon
(these are explained in EventHorizon's installation):

- `EventHorizon-read` permission is enough if you have separate user (perhaps `edgerouter-manager`?) for
  changing service discovery configs.
- Otherwise you need `EventHorizon-readwrite` permission


### Lambda functions

If you plan to use Edgerouter to proxy Lambda functions - let's say your functions are
`FunctionA` and `FunctionB`, you need an inline policy that looks like this:

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Sid": "edgeroterInvocableLambdas",
            "Effect": "Allow",
            "Action": "lambda:InvokeFunction",
            "Resource": [
                "arn:aws:lambda:us-east-1:123456789011:function:FunctionA",
                "arn:aws:lambda:us-east-1:123456789011:function:FunctionB"
            ]
        }
    ]
}
```

NOTE: replace `123456789011` with your account id!

(Pro-tip: you can replace `us-east-1` with `*` if you use multiple Lambda regions and you
want to make it easier to write these policies)


### S3 static website deployment

For the user that deploys static websites to S3 (`edgerouter-manager` OR `edgerouter`),
you need an inline policy like this (NOTE: replace `yourorg-staticwebsites` with your bucket name):

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Sid": "deployStaticWebsites",
            "Effect": "Allow",
            "Action": [
                "s3:PutObject",
                "s3:DeleteObject",
                "s3:PutObjectAcl"
            ],
            "Resource": [
                "arn:aws:s3:::yourorg-staticwebsites/*"
            ]
        }
    ]
}
```


Config required pre-start
-------------------------

Before starting Edgerouter, you need to assemble the following ENV variables to start
it with.

Configuration is driven by the following ENV variables:

- EventHorizon access: CertBus + service discovery of static applications
  * `AWS_ACCESS_KEY_ID`
  * `AWS_SECRET_ACCESS_KEY`
  * `CERTBUS_CLIENT_PRIVKEY`, base64 encoded PEM encoded ("----- BEGIN ... -----") private key
  * `EVENTHORIZON_TENANT`, example: prod:1
- Docker service discovery (**optional**)
  * `DOCKER_CLIENTCERT`, base64 encoded PEM encoded ("----- BEGIN ... -----") cert
  * `DOCKER_CLIENTCERT_KEY`, base64 encoded PEM encoded ("----- BEGIN ... -----") private key
  * `DOCKER_URL`, example: https://dockersockproxy:4431
  * `NETWORK_NAME`, example: fn61

### A note about Docker service discovery

If you're using Docker Swarm with multiple nodes, you probably need to run
[dockersockproxy](https://github.com/function61/dockersockproxy) (or similar) with a
deployment constraint to the Swarm manager node because for the Docker service discovery
to see all Swarm tasks' IPs we need to query the Swarm manager node.

If you're running a single node, you can probably just mount the Docker socket into
Edgerouter's container and set `DOCKER_URL=unix:///var/run/docker.sock`. In this case you
don't need `DOCKER_CLIENTCERT` or `DOCKER_CLIENTCERT_KEY`.


Runtime config
--------------

Most of the runtime config in Edgerouter is controllable by its dynamic service discovery
mechanism. Those changes are updated to each Edgerouter node (if you have a cluster, this
is important). Read the rest of the docs to become familiar with the mechanism.


Start Edgerouter
----------------

You are now ready to start. Use your favourite mechanism to start Docker containers. You
can find the image name and the latest version tag from the repo's README. Just remember
to pass the pre-start ENVs from this guide.

Example Docker config (ran in Docker Swarm):

```yaml
version: "3.5"
services:
  edgerouter:
    deploy:
      update_config:
        parallelism: 1
        order: start-first
      resources:
        limits:
          memory: "100663296"
    environment:
      AWS_ACCESS_KEY_ID: ...
      AWS_SECRET_ACCESS_KEY: ...
      CERTBUS_CLIENT_PRIVKEY: ...
      DOCKER_CLIENTCERT: ...
      DOCKER_CLIENTCERT_KEY: ...
      DOCKER_URL: https://dockersockproxy:4431
      EVENTHORIZON_TENANT: prod:1
      LOGGER_SUPPRESS_TIMESTAMPS: "1"
      NETWORK_NAME: fn61
    image: fn61/edgerouter:PUT_VERSION_TAG_HERE
    networks:
      default: null
    ports:
    - mode: ingress
      target: 80
      published: 80
      protocol: tcp
    - mode: ingress
      target: 443
      published: 443
      protocol: tcp
networks:
  default:
    external:
      name: fn61
```
