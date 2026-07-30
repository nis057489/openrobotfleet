# OpenRobotFleet

**Effortless orchestration for your robotics classroom or lab.**

## Screenshots

![Robots view screenshot](img/1.png)

![Robot detail screenshot](img/2.png)

![Scenarios / deployment screenshot](img/3.png)

![Golden Image Builder screenshot](img/4.png)

OpenRobotFleet helps instructors and lab managers maintain control over a fleet of robots (and laptops). Instead of manually SSH-ing into 30 robots to pull the latest code or restart a service, you can manage everything from a single web dashboard.

## Why use this?

* **Save Time**: Push code updates to your entire fleet in seconds, not hours.
* **Reduce Friction**: Reset robots for the next class with a "Semester Wizard" that wipes logs and updates code.
* **Stay Informed**: See at a glance which robots are online, their battery status (if reported), and what code they are running.
* **Unified Management**: Manage your robots and development laptops in one place.

## Key Features

### 🤖 Fleet Overview

Instantly see the status of every robot in your lab. Know their IP addresses, last seen times, and current operational status (and mood!) without scanning the network.

### 📦 One-Click Code Deployment ("Scenarios")

Define "Scenarios" (e.g., "Lab 1", "Midterm Project") that point to specific Git repositories and branches. Apply these scenarios to one robot or the whole fleet to ensure everyone is running the correct code.

### 🔄 Remote Control

* **Restart ROS**: specific services or the whole stack.
* **Reset Logs**: Clear out old log files to free up space.
* **WiFi Configuration**: Connect laptops/robots to the network remotely.

### 🎓 Semester Wizard

A dedicated tool for teaching assistants and instructors to batch-reset the fleet. Reinstall agents, wipe logs, update repositories, and apply specific scenarios (batch code deployment) for the new semester in one go.

### 💻 Laptop Support

Manage lab laptops just like robots. Push code updates and manage WiFi profiles on Ubuntu-based development machines.

## Getting Started

The fastest way to get a lab fleet up and running is:

1) build a **Golden Image**, 2) flash it onto every robot, 3) ensure everything is on the same **Layer 2** network, 4) define **YAML Scenarios** for the code you want deployed, then 5) manage the fleet from the dashboard.

### Quick Start (Golden Image + Scenarios)

#### 0) Prerequisites

* A machine to run the Controller (same network as the fleet)
* Docker + Docker Compose
* Your robots/laptops are reachable on the same Layer 2 network (same WiFi/VLAN/subnet)

#### 1) Configure and start the Controller (Docker)

1. Create a local env file:

```bash
    cp .env.example .env
```

2. Edit `.env` (most important: the network(s) to scan):

        - `SCAN_SUBNETS` controls what the Controller scans for SSH-able hosts (comma-separated).
            Example: `SCAN_SUBNETS=192.168.1.0/24,10.0.0.0/24`
        - `ADMIN_PASSWORD` sets the dashboard admin password.
        - If you set a real public domain, also set `ACME_EMAIL` to a real email (Let’s Encrypt rejects `example.com`).

3. Start the stack:

```bash
    docker compose up --build
```

4. Open the dashboard:

* Local: `https://localhost` (you may get a browser TLS warning)

#### 2) Build + flash a Golden Image

1. In the dashboard, go to **Golden Image**.
2. Fill in WiFi + Controller/MQTT settings.
3. Click **Build** (this produces the base image) and/or **Download** (this downloads the `user-data` cloud-init config used by the image).
4. Flash the resulting image onto every robot.

The Golden Image config bakes in the Agent configuration so robots come up pre-connected (no per-robot SSH install step).

#### 3) Power on robots on the same Layer 2 network

* Put the Controller machine and all robots/laptops on the same WiFi/VLAN/subnet.
* Ensure the Controller can reach them (and they can reach the Controller’s MQTT broker).

Once powered on, robots should start appearing in the dashboard as they connect and send status.

#### 4) Create YAML Scenarios (code deployment)

Scenarios are small YAML snippets that declare what git repo each robot/laptop should have.

Minimal example:

```yaml
repo:
        url: https://github.com/your-org/your-repo.git
```

Optional fields:

```yaml
repo:
        url: https://github.com/your-org/your-repo.git
        branch: main
        # Path is relative to the agent's workspace_path (robots default to /home/ubuntu/ros_ws/src)
        path: my-repo-folder
```

Create scenarios in the dashboard (**Scenarios**) and paste the YAML. Then apply the scenario to one robot to validate, and finally to the whole fleet.

#### 4b) Create Groups (DDS network isolation)

If you're running several independent robot+laptop pairs on the same Wi-Fi (e.g. a classroom lab), use the **Groups** page to pair each robot with its laptop under a dedicated `ROS_DOMAIN_ID`. This keeps every group's ROS 2 graph — topics, TF, discovery traffic — isolated from every other group, so one team's RViz never sees another team's robot and nobody can accidentally publish onto someone else's `/cmd_vel`.

* Every robot golden-imaged by this app already defaults to **Cyclone DDS** (`RMW_IMPLEMENTATION=rmw_cyclonedds_cpp`), the recommended middleware for this kind of lab deployment — it needs no extra infrastructure and has excellent TurtleBot/Nav2/RViz interoperability.
* Create a Group, pick its robot and laptop, and a domain ID is suggested automatically (11, 12, 13, ... one per group). Saving immediately pushes the domain ID and RMW setting to both devices over MQTT and restarts ROS — no reflash needed, even for robots already in the field.
* Leave **static peer discovery** off to start with; normal multicast discovery is fine for a lab of ~20 devices. Only turn it on for a group if multicast proves unreliable on your Wi-Fi — it locks that group's Cyclone DDS discovery to the robot's and laptop's known IPs instead (use fixed/static DHCP leases if you do this, so the IPs don't change).
* The **Download RViz Launcher** button on the Groups page produces a small `rviz-domain` script for the lab manager's laptop: `rviz-domain <group-name>` opens RViz on that group's domain so you can inspect any team's robot without touching their session, and `rviz-domain group-1 & rviz-domain group-2 &` opens several at once.
* For continuously-streaming sensor topics (LaserScan, camera, IMU, odometry, joint states), prefer Best Effort/Keep-Last-1 QoS and compressed image transport — the golden image installs the compressed-transport packages and drops an example `qos_overrides.example.yaml` into each robot's workspace as a starting point. Keep `cmd_vel`, services, and actions on the default Reliable QoS. This is scenario/launch-file code, so it isn't pushed centrally by the fleet manager.

**Wi-Fi is infrastructure this app doesn't control**, but it matters just as much: use a dedicated access point on 5 GHz with a fixed channel, and make sure client isolation is **disabled** (each laptop needs to reach its own robot directly). Avoid splitting robots onto 5 GHz and laptops onto 2.4 GHz. If you're stuck on university/enterprise Wi-Fi, DDS domain isolation is even more important, since you have less control over multicast behavior.

### 5) Install the Agent onto Laptops (and any non-golden imaged Robots)

* Use the **Scan Network** button under **Laptops** or **Robots** pages to find and enrol new devices

#### 6) Use the app

* Use **Robots** / **Laptops** to monitor status.
* Use **Scenarios** to deploy code.
* Use tools like **Semester Wizard** / **Restart ROS** / **Reset Logs** as needed.

## How it Works

1. **Install the Agent**: Use the "Add Robot" or "Add Laptop" tab in the dashboard. You'll need the IP address and SSH credentials of the target machine once. The system will install a lightweight agent that runs in the background.
2. **The Agent**: This small program runs on the robot, keeping it connected to your dashboard and listening for your commands.
3. **The Dashboard**: Your command center. It talks to the robots via a central server (included in the Docker setup).

## Common Tasks

### Adding a New Robot

Navigate to the **Robots** tab and click **Add Robot**. Enter the IP address, username (usually `ubuntu`), and SSH key/password. The manager will handle the rest.

### Deploying Code for a Class

1. Go to **Scenarios** and create a new Scenario.
2. Enter the Git URL (e.g., `https://github.com/your-course/lab1.git`) and the branch name.
3. Click **Apply**, select the robots, and watch them update.

### Fixing a "Stuck" Robot

If a robot is behaving strangely, try the **Restart ROS** command from the robot's detail page. If that fails, you can use the **Terminal** view (if configured) or check the logs remotely.

## Technical Details (For the curious)

Under the hood, this system uses:

* **Go**: For a fast, reliable backend and agent.
* **React**: For a responsive web interface.
* **MQTT**: For communication between robots, laptops and the server.
* **Batch Architecture**: Commands are bundled and sent to agents for atomic, sequential execution, ensuring reliability even with intermittent network connectivity.
* **SQLite**: For simple, self-contained data storage.
* **Secrets**: The dashboard might respond to a classic cheat code...

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md) for instructions on how to run the project locally for development.

## Contributing

Contributions are welcome! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for details.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

---
*Built for ROS 2 systems, including Turtlebots and most Ubuntu-based robots.*
