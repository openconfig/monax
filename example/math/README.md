# Math Example

The Monax math example is a collection of simple gRPC servers and a
corresponding Go test that demonstrate core Monax concepts and functionality.

## Introduction

[`math_test.go`](test/math_test.go) is an example of the lifecycle of a test that
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

*   [`abstract_sut.txtpb`](test/abstract_sut.txtpb): The interfaces required by the
    test. Monax will find components that provide these interfaces and make them
    available to the test.
*   [`kubernetes_library.txtpb`](test/kubernetes_library.txtpb): The components that
    Monax can use to fulfill the required interfaces.
*   [`kubernetes_runtime_parameters.txtpb`](test/kubernetes_runtime_parameters.txtpb):
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

For simplicity, set a variable that we will reuse in below commands to ensure
you're in the correct directory.

```shell
export MONAX_ROOT_DIR=/path/to/monax
```

## Running the test

Run the test using the included abstract SUT, library, and runtime
parameters:

```shell
cd "${MONAX_ROOT_DIR}/example/math/test"
go test -v math_test.go \
  --abstract_sut=abstract_sut.txtpb \
  --library=kubernetes_library.txtpb \
  --runtime_parameters=kubernetes_runtime_parameters.txtpb \
  --alsologtostderr
```

### Configuring an Image Repository Address

By default, Monax loads images locally into the specified kind cluster. If you
would instead prefer to push Docker images to a container registry (for
instance, Google Container Registry or Artifact Registry), configure the
`image_repository_address` field in the
[runtime parameters file](test/kubernetes_runtime_parameters.txtpb):

```text proto
kubernetes {
  image_repository_address: "gcr.io/my-project/monax-examples"
}
```

When specifying this option, Monax will push images to the given registry using
your local Docker credentials (`~/.docker/config.json`) and rewrite the
deployment templates to reference images from that location.

You will also need to disable the local kind loading for each service component:

```shell
for image in addition subtraction multiplication division; do
  printf ',s/load_to_kind: true/load_to_kind: false/g\nw\n' | ed -s "${MONAX_ROOT_DIR}/example/math/${image}/deploy/kubernetes.txtpb"
done
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

* Set the name of your kind Kubernetes cluster

  ```shell
  export KIND_CLUSTER="monax"
  ```

  > ❗ **IMPORTANT**: Use the actual name of your Kubernetes cluster.

* Install [grpcurl](https://github.com/fullstorydev/grpcurl)

  ```shell
  go install github.com/fullstorydev/grpcurl/cmd/grpcurl@master
  ```

* Build the Monax CLI

  Monax is written as a library to support automated testing. To use the same
  functionality in a manual case, there is a CLI that is used to start/stop
  the SUT and to get the endpoints `grpcurl` will be calling to.

  ```shell
  cd "${MONAX_ROOT_DIR}"
  go build -o monax_cli ./cli/
  ```

* Build the example images with Docker

  ```shell
  cd "${MONAX_ROOT_DIR}/example/math"
  for image in addition subtraction multiplication division; do
    docker build \
      --file "${image}/deploy/Dockerfile" \
      --tag "${image}:latest" \
      .
  done
  ```

* Upload images to your kind Kubernetes cluster

  ```shell
  cd "${MONAX_ROOT_DIR}/example/math"
  for image in addition subtraction multiplication division; do
    kind load docker-image "${image}" --name "${KIND_CLUSTER}"
  done
  ```

### Bringing up the SUT

The Monax CLI is a continuously running REPL CLI. This is to allow caching of
service addresses and other functionality.

Use the Monax CLI to bring up the SUT for testing. Notice that the flag values
are the same as running `math_test.go` above.

```shell
cd "${MONAX_ROOT_DIR}/example/math/test"
../../../monax_cli \
  --abstract_sut=abstract_sut.txtpb \
  --library=kubernetes_library.txtpb \
  --runtime_parameters=kubernetes_runtime_parameters.txtpb
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
  192.168.8.2:30754 \
  monax.example.addition.Addition/Add
```

Breakdown of the flags used:

* `-plaintext` - Use plain-text HTTP/2 when connecting to server (no TLS).
* `-d` - Data for request contents. In this case, a text proto string of the
  AddRequest from
  [addition/api/addition.proto](addition/api/addition.proto).
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

