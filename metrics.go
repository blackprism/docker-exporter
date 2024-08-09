package main

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/sourcegraph/conc/pool"
)

type Metrics struct {
	RootFS                 string
	VolumeConcurrency      int
	VolumeComputationLimit int64
	volumeComputationUsage int64
	volumeLastCallWindow   time.Time
}

func (m *Metrics) Metrics(w http.ResponseWriter, r *http.Request) {
	if time.Since(m.volumeLastCallWindow) >= 1*time.Minute {
		m.volumeLastCallWindow = time.Now()
		m.volumeComputationUsage = int64(math.Max(0, float64(m.volumeComputationUsage-m.VolumeComputationLimit)))
	}

	if m.volumeComputationUsage > m.VolumeComputationLimit {
		return
	}

	start := time.Now()

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		slog.Error("error getting docker client", slog.Any("error", err))
	}

	volumes, err := cli.VolumeList(context.Background(), volume.ListOptions{})
	if err != nil {
		slog.Error("failed to list volumes", slog.Any("error", err))
		return
	}

	var dockerVolumes []dockerVolumeSize

	p := pool.New().WithMaxGoroutines(m.VolumeConcurrency)
	for _, vol := range volumes.Volumes {
		p.Go(func() {
			cmd := exec.Command("./du", "-bs", m.RootFS+vol.Mountpoint)
			out, errCmd := cmd.Output()

			if errCmd != nil {
				slog.Error("failed to get directory size", slog.Any("error", errCmd))
				return
			}

			outSplitted := strings.SplitN(string(out), "\t", 2)

			dockerVolumes = append(dockerVolumes, dockerVolumeSize{
				Name:       vol.Name,
				Size:       outSplitted[0],
				MountPoint: vol.Mountpoint,
				Project:    vol.Labels["com.docker.compose.project"],
				Volume:     vol.Labels["com.docker.compose.volume"],
			})
		})
	}
	p.Wait()

	for _, dvs := range dockerVolumes {
		metric := fmt.Sprintf("docker_volume_size_bytes{name=%q,mountpoint=%q", dvs.Name, dvs.MountPoint)
		if dvs.Project != "" {
			metric += fmt.Sprintf(",project=%q", dvs.Project)
		}
		if dvs.Volume != "" {
			metric += fmt.Sprintf(",volume=%q", dvs.Volume)
		}
		metric += fmt.Sprintf("} %s", dvs.Size)

		_, errPrint := fmt.Fprintln(w, metric)

		if errPrint != nil {
			slog.Error("failed to output metric", slog.Any("error", errPrint))
			continue
		}
	}

	m.volumeComputationUsage += time.Since(start).Milliseconds()

	return
}
