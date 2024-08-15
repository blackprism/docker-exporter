package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"golang.org/x/sync/errgroup"
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
		slog.LogAttrs(context.Background(), slog.LevelError, "error getting docker client", slog.Any("error", err))
	}

	volumes, err := cli.VolumeList(context.Background(), volume.ListOptions{})
	if err != nil {
		slog.LogAttrs(context.Background(), slog.LevelError, "failed to list volumes", slog.Any("error", err))
		return
	}

	metrics := make(chan string)

	go func() {
		for metric := range metrics {
			_, write := io.WriteString(w, metric)
			if write != nil {
				return
			}
		}
	}()

	g, _ := errgroup.WithContext(r.Context())
	g.SetLimit(m.VolumeConcurrency)
	for _, vol := range volumes.Volumes {
		g.Go(func() error {
			var path strings.Builder
			path.WriteString(m.RootFS)
			path.WriteString(vol.Mountpoint)
			cmd := exec.Command("du", "-bs", path.String())
			//out, errCmd := cmd.Output()
			out, errCmd := cmd.Output()

			if errCmd != nil {
				println(errCmd.Error())
			}

			if errCmd != nil {
				slog.LogAttrs(context.Background(), slog.LevelError, "failed to get directory size", slog.Any("error", errCmd))
				return nil
			}

			outSplitted := bytes.SplitN(out, []byte("\t"), 2)

			if len(outSplitted) != 2 {
				slog.LogAttrs(context.Background(), slog.LevelError, "unexpected output from du command", slog.String("output", string(out)))
				return nil
			}

			metrics <- buildMetric(vol, outSplitted[0])
			return nil
		})
	}

	err = g.Wait()

	if err != nil {
		println(err.Error())
		return
	}

	m.volumeComputationUsage += time.Since(start).Milliseconds()
}

func buildMetric(vol *volume.Volume, size []byte) string {
	var metric strings.Builder
	metric.WriteString("docker_volume_size_bytes{name=")
	metric.WriteString(strconv.Quote(vol.Name))
	metric.WriteString(",mountpoint=")
	metric.WriteString(strconv.Quote(vol.Mountpoint))
	if vol.Labels["com.docker.compose.project"] != "" {
		metric.WriteString(",project=")
		metric.WriteString(strconv.Quote(vol.Labels["com.docker.compose.project"]))
	}
	if vol.Labels["com.docker.compose.volume"] != "" {
		metric.WriteString(",volume=")
		metric.WriteString(strconv.Quote(vol.Labels["com.docker.compose.volume"]))
	}
	metric.WriteString("} ")
	metric.Write(size)
	metric.WriteString("\n")

	return metric.String()
}
