#!/usr/bin/env python3
"""Top-bar indicator: does this laptop's clock agree with its robot's?

Installed and kept up to date by openrobotfleet-agent (internal/agent/
time_indicator.go); local edits are overwritten.

nav2, SLAM and RViz on the laptop compare their own clock with timestamps
from the robot, and fail ("transform data too old", empty costmaps) when the
two disagree. Rather than trusting "synchronised" flags, this measures the
real thing: every /scan the robot sends is stamped with the robot's clock, so
laptop time minus the stamp is the clock offset plus the network latency.
The smallest value over a few seconds is the offset plus the fastest
delivery, a few ms on the lab Wi-Fi.
"""

import os
import signal
import socket
import subprocess
import threading
import time
from collections import deque

import gi

gi.require_version("Gtk", "3.0")
gi.require_version("AyatanaAppIndicator3", "0.1")
from gi.repository import AyatanaAppIndicator3 as AppIndicator  # noqa: E402
from gi.repository import GLib, Gtk  # noqa: E402

ICON_DIR = "/usr/local/share/openrobotfleet/icons"
AGENT_SOCKET = "/run/openrobotfleet-agent.sock"
WINDOW = 3.0  # seconds of scans to take the minimum over
STALE = 5.0  # no scan for this long: no robot data
GOOD = 0.05  # |offset| below this is green
WARN = 0.2  # below this amber, else red (nav2 waits 0.1-0.3 s for TF)


class ScanOffset:
    """Tracks laptop-minus-robot clock offset from /scan timestamps."""

    def __init__(self):
        self.lock = threading.Lock()
        self.samples = deque()
        self.last = 0.0
        self.error = None

    def on_scan(self, msg):
        now = time.time()
        stamp = msg.header.stamp.sec + msg.header.stamp.nanosec * 1e-9
        with self.lock:
            self.samples.append((now, now - stamp))
            self.last = now

    def offset(self):
        now = time.time()
        with self.lock:
            while self.samples and now - self.samples[0][0] > WINDOW:
                self.samples.popleft()
            if not self.samples or now - self.last > STALE:
                return None, self.last
            return min(d for _, d in self.samples), self.last


def spin_ros(tracker):
    try:
        import rclpy
        from rclpy.qos import qos_profile_sensor_data
        from rclpy.signals import SignalHandlerOptions
        from sensor_msgs.msg import LaserScan

        # rclpy's own SIGTERM handler would only stop ROS, leaving the
        # indicator running and "systemctl stop" hanging; main() quits GTK.
        rclpy.init(signal_handler_options=SignalHandlerOptions.NO)
        node = rclpy.create_node("openrobotfleet_time_indicator")
        # Best effort matches both best-effort and reliable publishers.
        node.create_subscription(LaserScan, "/scan", tracker.on_scan, qos_profile_sensor_data)
        rclpy.spin(node)
    except Exception as e:  # shown in the menu; the indicator keeps running
        tracker.error = f"ROS unavailable: {e}"


def laptop_ntp():
    try:
        synced = subprocess.run(
            ["timedatectl", "show", "-p", "NTPSynchronized", "--value"],
            capture_output=True, text=True, timeout=5).stdout.strip() == "yes"
        server = subprocess.run(
            ["timedatectl", "show-timesync", "-p", "ServerName", "--value"],
            capture_output=True, text=True, timeout=5).stdout.strip()
    except Exception:
        return False, ""
    return synced, server


def ask_agent(request):
    """Sends one request to the local agent, which runs as root."""
    try:
        with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as s:
            s.settimeout(60)
            s.connect(AGENT_SOCKET)
            s.sendall(request.encode() + b"\n")
            return s.recv(512).decode().strip()
    except OSError as e:
        return f"error: agent not reachable ({e})"


def fmt(seconds):
    sign = "+" if seconds >= 0 else "−"
    s = abs(seconds)
    return f"{sign}{s * 1000:.0f} ms" if s < 1 else f"{sign}{s:.1f} s"


class Indicator:
    def __init__(self, tracker):
        self.tracker = tracker
        self.ind = AppIndicator.Indicator.new_with_path(
            "openrobotfleet-time", "time-grey", AppIndicator.IndicatorCategory.SYSTEM_SERVICES, ICON_DIR)
        self.ind.set_status(AppIndicator.IndicatorStatus.ACTIVE)
        self.ind.set_title("Robot clock sync")

        menu = Gtk.Menu()
        self.robot_item = Gtk.MenuItem(label="")
        self.laptop_item = Gtk.MenuItem(label="")
        self.scan_item = Gtk.MenuItem(label="")
        for item in (self.robot_item, self.laptop_item, self.scan_item):
            item.set_sensitive(False)
            menu.append(item)
        menu.append(Gtk.SeparatorMenuItem())
        self.sync_laptop = Gtk.MenuItem(label="")
        self.sync_laptop.connect("activate", lambda _: self.request("resync-time", "Syncing this laptop's clock\u2026", "Laptop clock synced"))
        menu.append(self.sync_laptop)
        self.fix_robot = Gtk.MenuItem(label="")
        self.fix_robot.connect("activate", lambda _: self.request(
            "fix-robot", "Asking the robot to resync\u2026", "Robot is resyncing and restarting ROS (about 30 s)"))
        menu.append(self.fix_robot)
        self.action_item = Gtk.MenuItem(label="")
        self.action_item.set_sensitive(False)
        menu.append(self.action_item)
        # Only while out of sync, so the buttons don't suggest a problem.
        self.hint_sep = Gtk.SeparatorMenuItem()
        menu.append(self.hint_sep)
        self.hint = Gtk.MenuItem(label="Out of sync: try the buttons above,\nthen relaunch nav2/SLAM on this laptop.")
        self.hint.set_sensitive(False)
        menu.append(self.hint)
        menu.show_all()
        self.action_item.hide()
        self.ind.set_menu(menu)

        self.ntp = (False, "")
        self.refresh_ntp()
        GLib.timeout_add_seconds(10, self.refresh_ntp)
        self.update()
        GLib.timeout_add(500, self.update)

    def request(self, request, busy, done):
        self.show_action(busy)

        def run():
            reply = ask_agent(request)
            text = done if reply == "ok" else reply.removeprefix("error: ")
            GLib.idle_add(self.show_action, text)
            self.refresh_ntp()

        threading.Thread(target=run, daemon=True).start()

    def show_action(self, text):
        self.action_item.set_label(text)
        self.action_item.show()
        return False

    def refresh_ntp(self):
        threading.Thread(target=lambda: setattr(self, "ntp", laptop_ntp()), daemon=True).start()
        return True

    def update(self):
        offset, last = self.tracker.offset()
        if offset is None:
            icon, label = "time-grey", "Robot: no data"
            robot = self.tracker.error or "Robot: no /scan received (is ROS running on the robot?)"
        else:
            # offset is laptop minus robot; show the robot relative to this laptop.
            robot_vs_laptop = -offset
            icon = "time-green" if abs(offset) < GOOD else "time-amber" if abs(offset) < WARN else "time-red"
            label = f"Robot {fmt(robot_vs_laptop)}"
            state = "in sync" if abs(offset) < GOOD else "slightly off" if abs(offset) < WARN else "OUT OF SYNC"
            robot = f"Robot clock {fmt(robot_vs_laptop)} vs this laptop: {state}"
        synced, server = self.ntp
        laptop = f"This laptop: synced to {server}" if synced else f"This laptop: NOT synced ({server or 'no server'})"
        ago = f"{time.time() - last:.1f} s ago" if last else "never"
        in_sync = icon == "time-green"
        if in_sync:
            self.sync_laptop.set_label("Resync this laptop's clock")
            self.fix_robot.set_label("Resync robot's clock and restart its ROS")
        else:
            self.sync_laptop.set_label("Sync this laptop's clock now")
            self.fix_robot.set_label("Fix robot: resync its clock and restart its ROS")
        self.hint_sep.set_visible(not in_sync)
        self.hint.set_visible(not in_sync)
        self.ind.set_icon_full(icon, label)
        self.ind.set_label(label, "Robot −888 ms")
        self.robot_item.set_label(robot)
        self.laptop_item.set_label(laptop)
        self.scan_item.set_label(f"Last robot scan: {ago}")
        return True


def main():
    tracker = ScanOffset()
    threading.Thread(target=spin_ros, args=(tracker,), daemon=True).start()
    Indicator(tracker)
    for sig in (signal.SIGTERM, signal.SIGINT):
        GLib.unix_signal_add(GLib.PRIORITY_DEFAULT, sig, Gtk.main_quit)
    Gtk.main()


if __name__ == "__main__":
    os.environ.setdefault("PYTHONUNBUFFERED", "1")
    main()
