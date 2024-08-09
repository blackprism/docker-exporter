package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strconv"

	"github.com/samber/oops"
)

var defaultPort = "9100"
var defaultRootFSDirectory = "/rootfs"
var defaultVolumeConcurrency = 10
var defaultVolumeComputationLimit = 10000

type dockerVolumeSize struct {
	Name       string
	Size       string
	MountPoint string
	Project    string
	Volume     string
}

func main() {
	ctx := context.Background()

	err := run(ctx, os.Getenv)

	if err != nil {
		slog.Error("failed to start docker-exporter", slog.Any("error", err))
		os.Exit(1)
	}
}

func run(ctx context.Context, getenv func(string) string) error {
	rootfs := getenv("ROOTFS_DIRECTORY")

	if rootfs == "" {
		rootfs = defaultRootFSDirectory
	}

	volumeConcurrency, err := strconv.Atoi(getenv("VOLUME_CONCURRENCY"))

	if volumeConcurrency == 0 || err != nil {
		volumeConcurrency = defaultVolumeConcurrency
	}

	volumeComputationLimit, err := strconv.Atoi(getenv("VOLUME_COMPUTATION_LIMIT"))

	if volumeComputationLimit == 0 || err != nil {
		volumeComputationLimit = defaultVolumeComputationLimit
	}

	m := Metrics{
		RootFS:                 rootfs,
		VolumeConcurrency:      volumeConcurrency,
		VolumeComputationLimit: int64(volumeComputationLimit),
	}

	http.HandleFunc("/metrics", m.Metrics)

	port := getenv("PORT")

	if port == "" {
		port = defaultPort
	}

	errServer := http.ListenAndServe(":"+port, nil)

	if errServer != nil {
		return oops.Wrapf(errServer, "failed to start http server")
	}

	return nil
}
