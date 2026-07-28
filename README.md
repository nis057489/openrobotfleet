# OpenRobotFleet

> **Manage an entire robotics lab from one browser.**
>
> Deploy code, monitor robots, prepare practical classes, and reset your fleet in minutes—not hours.

OpenRobotFleet is a fleet management platform for ROS 2 teaching laboratories and research groups. It removes the repetitive work involved in maintaining multiple robots so instructors, researchers and teaching assistants can spend more time teaching and experimenting.

Whether you're running 6 TurtleBots for a lab class or dozens of robots across multiple projects, OpenRobotFleet helps keep every machine in a known, consistent state.

---

## Contents

* [Get Started](#get-started-in-under-30-minutes)
* [Screenshots](#screenshots)
* [Why OpenRobotFleet?](#why-academics-use-openrobotfleet)
* [Features](#features)
* [Typical Workflows](#typical-workflows)
* [Deployment](#deployment)
* [Flashing a Golden Image](#flashing-a-golden-image)
* [Creating a Scenario](#creating-a-scenario)
* [Under the Hood](#under-the-hood)
* [Contributing](#contributing)
* [License](#license)

---

## Built for robotics labs

Managing a robotics fleet usually means:

* SSH-ing into every robot before class
* Pulling Git repositories one machine at a time
* Restarting ROS when something breaks
* Resetting robots after every practical
* Wondering which robot is running which code

OpenRobotFleet automates these everyday tasks.

Instead of managing robots individually, you manage the fleet.

---

# Get started in under 30 minutes

Most labs can be running in four steps.

### 1. Start OpenRobotFleet

Run the Controller using Docker.

```bash
cp .env.example .env
docker compose pull
docker compose up -d
```

### 2. Build a preconfigured robot image

Use the built-in Golden Image Builder to create a reusable robot image containing the OpenRobotFleet Agent and your lab configuration.

Flash the same image onto each robot.

See [Flashing a Golden Image](#flashing-a-golden-image) for SD card instructions.

### 3. Power on your robots

Robots automatically connect to the Controller and appear in the dashboard.

No manual per-robot installation or SSH setup is required.

### 4. Deploy your course or research code

Create a Scenario pointing at your Git repository.

Click **Deploy**.

Every selected robot updates itself automatically.

---

# Screenshots

![Fleet Overview](img/1.png)

![Robot Details](img/2.png)

![Deployment Scenarios](img/3.png)

![Golden Image Builder](img/4.png)

---

# Why academics use OpenRobotFleet

## Save hours before every laboratory

Deploy software updates to every robot simultaneously instead of repeating the same commands dozens of times.

---

## Start every class from a known state

The Semester Wizard prepares an entire fleet by:

* updating repositories
* clearing logs
* reinstalling agents
* applying teaching scenarios

Perfect for practical classes.

---

## Know what's happening at a glance

See every robot from one dashboard.

Monitor:

* online/offline status
* battery level
* deployed software
* last contact time
* IP address
* health information

---

## Keep students on the same software version

Scenarios ensure every robot receives exactly the same code.

No more "it works on Robot 4 but not Robot 7."

---

## Manage robots and laptops together

Ubuntu development laptops appear alongside robots, making it easy to keep an entire teaching lab synchronised.

---

# Features

## Fleet Dashboard

Monitor every robot from a single web interface.

---

## Scenarios

Reusable deployment profiles for:

* laboratory exercises
* assignments
* demonstrations
* research experiments

Each Scenario simply references a Git repository and optional branch.

Deploy to one robot or the whole fleet.

---

## Golden Image Builder

Create a reusable base image containing:

* WiFi configuration
* Controller settings
* Fleet agent

Flash once and every robot automatically joins the fleet.

---

## Semester Wizard

Prepare an entire laboratory for a new semester with a single operation.

---

## Remote Administration

Without opening SSH you can:

* restart ROS
* clear logs
* configure WiFi
* deploy software
* monitor robot status

---

# Typical workflows

## Before a practical class

1. Deploy the "Lab 4" Scenario.
2. Wait for updates.
3. Verify every robot is online.

Done.

---

## Between student groups

Run Semester Wizard.

Every robot is reset and ready for the next class.

---

## During research

Deploy experimental branches to selected robots while leaving the rest of the fleet untouched.

Track exactly which robot is running which experiment.

---

# Deployment

## Requirements

* Docker + Docker Compose
* Ubuntu-based robots or laptops
* Layer-2 network, such as the same WiFi, VLAN, or subnet
* Raspberry Pi Imager, if flashing robot SD cards

## 1. Configure the Controller

Copy the example environment file:

```bash
cp .env.example .env
```

Edit:

* `SCAN_SUBNETS`
* `ADMIN_PASSWORD`

`SCAN_SUBNETS` tells OpenRobotFleet which networks to scan for reachable robots and laptops.

Example:

```bash
SCAN_SUBNETS=192.168.1.0/24,10.0.0.0/24
```

## 2. Start the Controller

The Controller image is built by GitHub Actions and published to GitHub Container Registry, so you only need to pull it — no local Go/Node/SQLite toolchain required. It's a multi-arch image (`linux/amd64` + `linux/arm64`), so the right variant is pulled automatically whether you're on an x86_64 server or an ARM64 board (e.g. Raspberry Pi, GL-MT6000).

```bash
docker compose pull
docker compose up -d
```

To update after a new release:

```bash
docker compose pull controller
docker compose up -d controller
docker image prune -f
```

Then open:

```text
https://localhost:8443
```

(or `https://<router-or-host-ip>:8443` if you're not running Docker locally). Traefik is mapped to host ports 8080/8443 instead of 80/443 by default so it doesn't collide with a web server or admin UI already running on the host (e.g. OpenWrt's LuCI on a router). If ports 80/443 are free on your machine, you can change the `traefik` service's `ports:` in `docker-compose.yml` back to `"80:80"` / `"443:443"` and open `https://localhost` instead — note this also affects whether the Let's Encrypt HTTP-01 challenge can succeed, since it requires port 80 to be reachable.

Your browser may show a local TLS warning when using the self-signed certificate.

## 3. Build a Golden Image

In the dashboard, open **Golden Image**.

Configure:

* WiFi settings
* Controller address
* MQTT settings
* Agent settings

Then build or download the generated image.

The resulting image can be flashed onto each robot so that every machine boots with the correct OpenRobotFleet configuration.

## Container images

Every push to `main` and every `v*` tag is built by [.github/workflows/docker.yml](.github/workflows/docker.yml) and published to GHCR:

```text
ghcr.io/nis057489/openrobotfleet-controller:latest    # tracks main
ghcr.io/nis057489/openrobotfleet-controller:v0.1.0    # immutable release tag
ghcr.io/nis057489/openrobotfleet-controller:sha-abc1234
```

Images are multi-arch (`linux/amd64`, `linux/arm64`) and public, so `docker compose pull` works without authentication. For production deployments, prefer pinning to a `vX.Y.Z` tag over `latest` so a new push to `main` doesn't silently replace the running Controller.

## Development

Changing OpenRobotFleet's code, not just running it? See [CONTRIBUTING.md](CONTRIBUTING.md) for running the Controller/frontend/agent locally and building the container image yourself.

---

# Flashing a Golden Image

After building a Golden Image from the OpenRobotFleet dashboard, write it to each robot's microSD card using Raspberry Pi Imager.

## Requirements

* Raspberry Pi Imager
* A microSD card, 32 GB or larger recommended
* An SD card reader

## 1. Open Raspberry Pi Imager

Launch **Raspberry Pi Imager**.

Select:

**Choose OS → Use custom**

Then browse to the Golden Image generated by OpenRobotFleet.

This will usually be a `.img` or `.img.xz` file.

> Raspberry Pi Imager can flash compressed `.img.xz` files directly. You do not need to extract them first.

## 2. Select the storage device

Click **Choose Storage** and select the target microSD card.

> **Warning:** This will erase all data currently on the card.

## 3. Flash the image

Click **Next**, then **Write**.

Raspberry Pi Imager will automatically:

* write the image
* verify the written data
* notify you when flashing is complete

This may take several minutes depending on the speed of the SD card and card reader.

## 4. Insert the card into the robot

Safely eject the microSD card, insert it into the robot, and power it on.

If your Golden Image was configured correctly, the robot will automatically:

* connect to the configured WiFi network
* start the OpenRobotFleet Agent
* appear in the OpenRobotFleet dashboard within a few moments

Repeat this process for each robot in your fleet.

---

# Creating a Scenario

Scenarios describe which software should be deployed to one or more robots.

A Scenario is a small YAML configuration that points to a Git repository.

Minimal example:

```yaml
repo:
  url: https://github.com/your-org/your-repository.git
```

With optional branch and workspace path:

```yaml
repo:
  url: https://github.com/your-org/your-repository.git
  branch: main
  path: my-package
```

Create the Scenario in the dashboard, then apply it to one robot or your entire fleet with a single click.

A good practice is to deploy a new Scenario to one robot first, verify it, and then deploy it to the full fleet.

---

# Under the hood

OpenRobotFleet is built using:

* Go
* React
* MQTT
* SQLite

Agents communicate with the Controller using reliable batch-based execution, allowing commands to complete even when connectivity is intermittent.

---

# Contributing

Contributions are welcome.

See `CONTRIBUTING.md`.

---

# License

MIT

---

*Designed for ROS 2 teaching laboratories, research groups and Ubuntu-based robot fleets.*
