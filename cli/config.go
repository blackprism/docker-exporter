package cli

import (
	"flag"
	"fmt"
)

var port *string
var volumeConcurrency *int
var volumeComputationLimit *int
var dockerComposeOnly *bool
var volumeEnabled *bool
var childProcessEnabled *bool

func init() {
	port = flag.String("port", "9100", "port to listen on")
	volumeConcurrency = flag.Int("volume-concurrency", 10, "maximum number of volumes to compute concurrently")
	volumeComputationLimit = flag.Int("volume-computation-limit", 10000, "maximum time (ms) allowed to compute volume within 1mn before next call will be skipped")
	dockerComposeOnly = flag.Bool("docker-compose-only", false, "Only report docker-compose containers")
	volumeEnabled = flag.Bool("volume", false, "Enable volume report")
	childProcessEnabled = flag.Bool("child-process", false, "Enable child process report")

	flag.Parse()
}

func MustGetParameter(parameter string) any {

	if parameter == "port" {
		return *port
	}

	if parameter == "volume-enabled" {
		return *volumeEnabled
	}

	if parameter == "child-process-enabled" {
		return *childProcessEnabled
	}

	if parameter == "volume-concurrency" {
		return *volumeConcurrency
	}

	if parameter == "volume-computation-limit" {
		return *volumeComputationLimit
	}

	if parameter == "docker-compose-only" {
		return *dockerComposeOnly
	}

	panic(fmt.Sprintf("parameter not found: %v", parameter))
}
