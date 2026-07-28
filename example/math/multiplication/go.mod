module github.com/openconfig/monax/example/math/multiplication

go 1.25.0

replace github.com/openconfig/monax/example/math/addition => ../addition

require (
	github.com/golang/glog v1.2.5
	github.com/openconfig/monax/example/math/addition v0.0.0-00010101000000-000000000000
	golang.org/x/net v0.55.0
	google.golang.org/grpc v1.82.1
	google.golang.org/protobuf v1.36.11
)

require (
	golang.org/x/sys v0.45.0 // indirect
	golang.org/x/text v0.37.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260414002931-afd174a4e478 // indirect
)
