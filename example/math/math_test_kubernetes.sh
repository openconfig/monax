#!/bin/bash

# Copyright 2026 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     https://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Run the math test in an existing Kubernetes cluster authenticated with the
# default kubeconfig file found at "${HOME}/.kube/config".

readonly SCRIPT_DIR="$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"

function build_docker_images() {
  for image in addition subtraction multiplication division; do
    if ! docker build \
        --file "example/math/${image}/deploy/Dockerfile" \
        --tag "${image}:latest" \
        --build-arg HTTP_PROXY="${HTTP_PROXY}" \
        --build-arg HTTPS_PROXY="${HTTPS_PROXY}" \
        --build-arg NO_PROXY="${NO_PROXY}" \
        .; then
      echo "Could not build ${image}" >&2
      exit 1
    fi
    if ! kind load docker-image "${image}" --name "${KIND_CLUSTER}"; then
      echo "Could not load ${image} into cluster ${KIND_CLUSTER}" >&2
      exit 1
    fi
  done
}

function run_math_test() {
  go test -v example/math/math_test.go \
    --abstract_sut=abstract_sut.txtpb \
    --library=kubernetes_library.txtpb \
    --runtime_parameters=kubernetes_runtime_parameters.txtpb \
    --alsologtostderr # Used by glog to expose Monax lib logs.
}

function main() {
  cd "${SCRIPT_DIR}"
  cd ../.. # cd back to base Monax directory

  if [[ -z "${KIND_CLUSTER}" ]]; then
    echo "Error: KIND_CLUSTER is not set." >&2
    echo "Please set 'KIND_CLUSTER=your_kind_cluster_name' before running this script." >&2
    echo "See the README to run math_test.go manually if not using \"kind\"." >&2
    exit 1
  fi

  build_docker_images

  run_math_test
}

main "$@"
