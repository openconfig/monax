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

# Usage:
#
# $ cd monax
# $ docker build \
#     -f example/math/subtraction/deploy/Dockerfile \
#     -t subtraction:latest \
#     .
#
# Optionally, if local environment uses a proxy:
#
# $ docker build \
#     -f example/math/subtraction/deploy/Dockerfile \
#     -t subtraction:latest \
#     --build-arg HTTP_PROXY=${HTTP_PROXY} \
#     --build-arg HTTPS_PROXY=${HTTPS_PROXY} \
#     --build-args NO_PROXY=localhost,.svc,.cluster.local,0.0.0.0/8,10.0.0.0/8,127.0.0.0/8,192.168.0.0/16 \
#     .
#
# IPv4 Bogon IP Address ranges included above as an example.
# See https://ipinfo.io/bogon for the full list, including IPv6 ranges.

FROM golang

WORKDIR /app

# Setup to reuse local proxy settings, if present. No-op otherwise.
ARG HTTP_PROXY
ARG HTTPS_PROXY
ARG NO_PROXY
ENV HTTP_PROXY=${HTTP_PROXY}
ENV http_proxy=${HTTP_PROXY}
ENV HTTPS_PROXY=${HTTPS_PROXY}
ENV https_proxy=${HTTPS_PROXY}
ENV NO_PROXY=${NO_PROXY}
ENV no_proxy=${NO_PROXY}
RUN if [ -n "${HTTP_PROXY}" ]; then \
        echo "Acquire::http::Proxy \"${HTTP_PROXY}\";" >> /etc/apt/apt.conf.d/proxy.conf; \
    fi; \
    if [ -n "${HTTPS_PROXY}" ]; then \
        echo "Acquire::https::Proxy \"${HTTPS_PROXY}\";" >> /etc/apt/apt.conf.d/proxy.conf; \
    fi

# Install protoc. The apt-get packages are too old to work with proto "edition".
RUN apt-get update \
 && DEBIAN_FRONTEND=noninteractive \
    apt-get install --no-install-recommends --assume-yes \
        unzip
RUN curl -LO https://github.com/protocolbuffers/protobuf/releases/download/v33.5/protoc-33.5-linux-x86_64.zip
RUN unzip protoc-33.5-linux-x86_64.zip -d /usr/local && rm protoc-33.5-linux-x86_64.zip
RUN chmod 755 -R /usr/local/bin

RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11
RUN go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.1

COPY ./subtraction .
COPY ./addition ../addition

RUN go mod tidy
RUN go mod download

RUN mkdir /app/out
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/out ./...

USER 1000:1000

ENTRYPOINT ["/app/out/cmd", "-alsologtostderr"]
