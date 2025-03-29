# docker-exporter

[![Docker Pulls](https://img.shields.io/docker/pulls/blackprism/docker-exporter.svg)](https://hub.docker.com/r/blackprism/docker-exporter)

`docker-exporter` is a Prometheus exporter that exposes detailed metrics about running Docker containers. It provides information on CPU usage, memory usage, network usage, I/O, volume size for each container, as well as for child processes.

## Features

* **Detailed per-container metrics:**
    * CPU usage (user, kernel)
    * Memory usage
    * Network usage (bytes received/sent)
    * I/O usage (bytes read/written)
    * Optional volume metrics
    * Optional child process metrics
* **Flexible configuration:**
    * Ability to filter monitored containers (Docker Compose only)

## Installation and Usage

### Prerequisites

* Docker installed and running
* Prometheus installed and configured

### Installation

1.  **Pull the Docker image:**

    ```bash
    docker pull blackprism/docker-exporter
    ```

2.  **Run the `docker-exporter` container with configuration flags:**

    ```bash
     docker run -d \
        -p 9100:9100 \
        -v /var/lib/docker:/var/lib/docker \
        -v /var/run/docker.sock:/var/run/docker.sock \
        blackprism/docker-exporter \
        --port=9100 \
        --rootfs=/ \
        --volume=true
    ```

### Prometheus Configuration

Add the `docker-exporter` target to your Prometheus configuration (`prometheus.yml`):

```yaml
scrape_configs:
  - job_name: 'docker'
    static_configs:
      - targets: ['<docker-exporter-ip>:9100']
