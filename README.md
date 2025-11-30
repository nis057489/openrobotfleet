# Turtlebot Fleet Manager

**Effortless orchestration for your robotics classroom or lab.**

Turtlebot Fleet Manager helps instructors and lab managers maintain control over a fleet of robots (and laptops). Instead of manually SSH-ing into 30 robots to pull the latest code or restart a service, you can manage everything from a single web dashboard.

## Why use this?

* **Save Time**: Push code updates to your entire fleet in seconds, not hours.
* **Reduce Friction**: Reset robots for the next class with a "Semester Wizard" that wipes logs and updates code.
* **Stay Informed**: See at a glance which robots are online, their battery status (if reported), and what code they are running.
* **Unified Management**: Manage your Turtlebots and development laptops in one place.

## Key Features

### 🤖 Fleet Overview

Instantly see the status of every robot in your lab. Know their IP addresses, last seen times, and current operational status without scanning the network.

### 📦 One-Click Code Deployment ("Scenarios")

Define "Scenarios" (e.g., "Lab 1", "Midterm Project") that point to specific Git repositories and branches. Apply these scenarios to one robot or the whole fleet to ensure everyone is running the correct code.

### 🔄 Remote Control

* **Restart ROS**: specific services or the whole stack.
* **Reset Logs**: Clear out old log files to free up space.
* **WiFi Configuration**: Connect laptops/robots to the network remotely.

### 🎓 Semester Wizard

A dedicated tool for teaching assistants and instructors to batch-reset the fleet. Reinstall agents, wipe logs, and checkout the starter code for the new semester in one go.

### 💻 Laptop Support

Manage lab laptops just like robots. Push code updates and manage WiFi profiles on Ubuntu-based development machines.

## Getting Started

The easiest way to run the Fleet Manager is using Docker.

### Using Docker

1. **Prerequisites**: Ensure you have Docker and Docker Compose installed.
2. **Start the System**:

    ```bash
    docker compose up --build
    ```

3. **Access the Dashboard**: Open your browser to `http://localhost:8080`.

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
* **MQTT**: For real-time communication between robots and the server.
* **SQLite**: For simple, self-contained data storage.

---
*Built for the Turtlebot 3 and 4, but compatible with most Ubuntu-based ROS 2 systems.*
