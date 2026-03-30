# Math Example

The Monax math example is a collection of simple gRPC servers and a
corresponding Go test that demonstrate core Monax concepts and functionality.

## Introduction

[`math_test.go`](math_test.go) is an example of the lifecycle of a test that
needs to talk to running services. It is a Go test that uses Monax to start a
SUT consisting of simple gRPC servers in a Kubernetes cluster. The servers
provide simple math operations, and the test makes calls to those servers to
verify their functionality.

The test has three phases:

1.  `TestMain` uses `monaxtest.Start(...)` to create a SUT, start the SUT, and
    check the SUT's status. There is also a `defer sut.Stop(ctx)` to bring down
    the SUT after the test.

1.  The body of the tests get a connection and create a client for the service
    under test.

    ```go
    // Example
    conn, err := sut.Interfaces().GRPC(ctx, "monax.example.addition.Addition")
    ...
    additionClient := additiongrpc.NewAdditionClient(conn)
    ```

1.  The test cases exercise the services via the clients, making calls and
    checking results.

Three configuration files determine how Monax constructs the SUT:

*   [`abstract_sut.txtpb`](abstract_sut.txtpb): The interfaces required by the
    test. Monax will find components that provide these interfaces and make them
    available to the test.
*   [`kubernetes_library.txtpb`](kubernetes_library.txtpb): The components that
    Monax can use to fulfill the required interfaces.
*   [`kubernetes_runtime_parameters.txtpb`](kubernetes_runtime_parameters.txtpb):
    Configures the Kubernetes runtime in Monax with information like Kubernetes
    config path.

The test can be run manually or with the provided script.

## Prerequisites

You must have:

*   A running [Kubernetes](https://kubernetes.io/) cluster
*   A `${HOME}/.kube/config` file that points to your cluster
*   Some way to build [Open Container Initiative](https://opencontainers.org/)
    images
*   Some way to push container images to your cluster

Set the name of your kind Kubernetes cluster:

```shell
export KIND_CLUSTER="monax"
```

> ❗ **IMPORTANT**: Use the actual name of your Kubernetes cluster.

## Running the test via script

The [`math_test_kubernetes.sh`](math_test_kubernetes.sh) script builds the
Docker images, loads them into the kind Kubernetes cluster, and runs the test.

### Prerequisites

In addition to the general prerequisites above, the script has additional
requirements:

*   [Docker](https://www.docker.com/) must be installed and the `docker` command
    must be available
*   [kind](https://kind.sigs.k8s.io/) must be installed and the `kind` command
    must be available

### Run math_test_kubernetes.sh

To run the script, simply call it:

```shell
./example/math/math_test_kubernetes.sh
```

The script goes through a number of stages:

*   Build Docker images
*   Push the images to your kind Kubernetes cluster
*   Run the math test

At the end of the test, you should see `go test` report "ok" and then the clean
up steps:

```text
...  # Many lines of the test case names and their status.
PASS
ok      command-line-arguments  13.202s
```

> ⓘ **NOTE**: If your local dev environment uses a proxy server, run the script
> with `env` variables `HTTP_PROXY`, `HTTPS_PROXY`, and `NO_PROXY` exported.
> See https://ipinfo.io/bogon for the full list of Bogon IPv4 and IPv6 addresses
> to include in `NO_PROXY`.

## Running the test manually

To run the test manually, do the following:

### 1. Build the example/math images

Use the `docker` CLI to build each of the math example images:

```shell
for image in addition subtraction multiplication division; do
  docker build \
    --file "example/math/${image}/deploy/Dockerfile" \
    --tag "${image}:latest" \
    .
done
```

> ⓘ **NOTE**: If you don't use `docker`, use your normal workflow commands to
> build the images.

### 2. Push the example/math images

Use the `kind` CLI to push each of the images to your Kubernetes cluster:

```shell
for image in addition subtraction multiplication division; do
  kind load docker-image "${image}" --name "${KIND_CLUSTER}"
done
```

> ⓘ **NOTE**: If you don't use `kind`, use your normal workflow commands to push
> images so that they are available to your cluster.

### 3. Run math_test.go

Finally, run the test using the included abstract SUT, library, and runtime
parameters:

```shell
go test -v example/math/math_test.go \
  --abstract_sut=abstract_sut.txtpb \
  --library=kubernetes_library.txtpb \
  --runtime_parameters=kubernetes_runtime_parameters.txtpb \
  --alsologtostderr
```

At the end of the test, you should see `go test` report "ok" and then the clean
up steps:

```text
...  # Many lines of the test case names and their status.
PASS
ok      command-line-arguments  13.202s
```

## Manual calls to SUT Services

Follow these steps to make manual calls to the SUT for discovery or
experimentation.

### Prerequisites

The math example services use gRPC as the interface. To make manual calls, we
need a CLI capable of using that protocol. For simplicity, we recommend the
use of [grpcurl](https://github.com/fullstorydev/grpcurl).

* Install [grpcurl](https://github.com/fullstorydev/grpcurl)

  ```shell
  go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest
  ```

* Build the Monax CLI

  Monax is written as a library to support automated testing. To use the same
  functionality in a manual case, there is a CLI that is used to start/stop
  the SUT and to get the endpoints `grpcurl` will be calling to.

  ```shell
  go build -o monax_cli ./cli/
  ```

> ⓘ **NOTE**: If the below steps have already been done from above, they do not
> need to be run again.

* Build the example images with Docker

  ```shell
  for image in addition subtraction multiplication division; do
    docker build \
      --file "example/math/${image}/deploy/Dockerfile" \
      --tag "${image}:latest" \
      .
  done
  ```

* Upload images to your kind Kubernetes cluster

  ```shell
  for image in addition subtraction multiplication division; do
    kind load docker-image "${image}" --name "${KIND_CLUSTER}"
  done
  ```

### Bringing up the SUT

The Monax CLI is a continuously running REPL CLI. This is to allow caching of
service addresses and other functionality.

Use the Monax CLI to bring up the SUT for testing. Notice that the flag values
are the same as running `math_test.go` above.

> ⓘ **NOTE**: Run in the `monax` directory. This will continuously run after
> each command.

```shell
./monax_cli \
  --abstract_sut=example/math/abstract_sut.txtpb \
  --library=example/math/kubernetes_library.txtpb \
  --runtime_parameters=example/math/kubernetes_runtime_parameters.txtpb
```

Start the SUT with the CLI. The `start` command will try to bring up all
services defined in the `abstract_sut` provided and their dependencies.

```shell
monax> start
I0131 00:38:43.694989  479551 sut.go:110] Starting SUT
...
SUT started successfully.
```

If any error messages were seen, use the `stop` command to return to a clean
state and try again.

### Getting the address of services

The math example services all use gRPC interfaces. To get the gRPC `host:port`
address, run the following command in the Monax CLI:

```shell
monax> targets grpc monax.example.addition.Addition
monax.example.addition.Addition: 192.168.8.50:50051
```

Lookup the service using the same `service_name` used in the abstract SUT
definition. Double check that it is for the same interface type (e.g., `http`).

### Making a gRPC call

With the address, we can use the `grpcurl` CLI to manually make API calls.

Open a new terminal instance and run the following command:

```shell
grpcurl \
  -plaintext \
  -d '{"augend": 5, "addend": 2}' \
  192.168.8.50:50051 \
  monax.example.addition.Addition/Add
```

Breakdown of the flags used:

* `-plaintext` - Use plain-text HTTP/2 when connecting to server (no TLS).
* `-d` - Data for request contents. In this case, a text proto string of the
  AddRequest from [addition/api/addition.proto](addition/api/addition.proto).
* `192.168.8.50:50051` - This is the `host:port` returned from the `targets`
  command above.
* `monax.example.addition.Addition/Add` - A `rpc` endpoint defined for the
  `Addition` service from
  [addition/api/addition.proto](addition/api/addition.proto).

> ⓘ **NOTE**: If needed, refer to the `grpcurl` documentation for adding
> credentials to this call.

### Cleanup

Once manual testing of the SUT is complete, remember to shut down the services.

Return to the terminal running the Monax CLI and run `stop` to stop the services
and `exit` to quit the CLI.

```shell
monax> stop
I0131 00:53:16.899432  485260 sut.go:142] Stopping SUT
...
SUT stopped successfully.

monax> exit
```

If the CLI was closed without shutting down, either use `kubectl delete` to
remove the generated `ConfigMap`, `Deployment`, and `Service` objects or run the
Monax CLI again with the `start` and then `stop` commands.

