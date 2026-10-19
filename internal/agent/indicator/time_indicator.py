#!/usr/bin/env python3
"""Top-bar indicator: is this laptop ready to run ROS with its robot?

Installed and kept up to date by openrobotfleet-agent (internal/agent/
time_indicator.go); local edits are overwritten.

nav2, SLAM and RViz on the laptop compare their own clock with timestamps
from the robot, and fail ("transform data too old", empty costmaps) when the
two disagree or the robot's data arrives late. Rather than trusting
"synchronised" flags, this measures the real thing: every /scan the robot
sends is stamped with the robot's clock, so laptop time minus the stamp is
the clock offset plus the delivery delay. Over a few seconds, the smallest
value is the offset (plus the fastest delivery, a few ms on the lab Wi-Fi)
and the median minus the smallest is the typical delivery delay.

evaluate() turns what the indicator can observe into the one state that is
most true right now; see its docstring for the states in priority order.
"""

import os
import signal
import socket
import statistics
import subprocess
import threading
import time
from collections import deque
from dataclasses import dataclass

ICON_DIR = "/usr/local/share/openrobotfleet/icons"
AGENT_SOCKET = "/run/openrobotfleet-agent.sock"

WINDOW = 3.0  # seconds of scans to measure over
STALE = 5.0  # no scan for this long: no robot data
GOOD = 0.05  # |offset| below this: clocks agree
WARN = 0.2  # below this: slightly off; above: out of sync (nav2 waits 0.1-0.3 s)
SLOW = 0.15  # typical delivery delay above this: data late

# Fix-robot phases (see Indicator.advance_fix).
STOP_GAP = 2.0  # scans missing this long: the robot's ROS has stopped
STOP_TIMEOUT = 45.0  # from the request: the robot never stopped
START_TIMEOUT = 150.0  # from the request: the robot never came back
SETTLE = 8.0  # fresh scans needed before trusting the result; a clock step
# during the resync can trigger a second restart by the robot's agent


class ScanOffset:
    """Collects (arrival, laptop-minus-robot delay) for recent /scan messages."""

    def __init__(self):
        self.lock = threading.Lock()
        self.samples = deque()
        self.last = 0.0  # arrival of the latest scan, 0 if none yet
        self.error = None  # set if ROS could not start

    def on_scan(self, msg):
        now = time.time()
        stamp = msg.header.stamp.sec + msg.header.stamp.nanosec * 1e-9
        with self.lock:
            self.samples.append((now, now - stamp))
            self.last = now

    def reset(self):
        """Forgets measurements, e.g. after a clock on either side changed."""
        with self.lock:
            self.samples.clear()

    def snapshot(self, now):
        """Returns (offset, typical delivery delay, last arrival); the first
        two are None without samples in the window."""
        with self.lock:
            while self.samples and now - self.samples[0][0] > WINDOW:
                self.samples.popleft()
            delays = [d for _, d in self.samples]
            last = self.last
        if not delays:
            return None, None, last
        lowest = min(delays)
        return lowest, statistics.median(delays) - lowest, last


@dataclass
class Observed:
    now: float
    started: float  # when the indicator started
    ros_error: str  # non-empty if ROS is unusable here
    paired: bool  # this laptop has a ROS domain to share with a robot
    offset: float  # laptop minus robot, None if no recent scans
    latency: float  # typical delivery delay, None if no recent scans
    last_scan: float  # 0 if never
    laptop_synced: bool
    laptop_server: str
    action: str  # "" or a busy message from a button in progress


@dataclass
class State:
    icon: str  # time-green / amber / red / grey / busy
    label: str  # next to the icon
    robot: str  # menu line about the robot
    ok: bool  # everything ready: buttons read as routine, not as fixes


def fmt(seconds):
    sign = "+" if seconds >= 0 else "−"
    s = abs(seconds)
    return f"{sign}{s * 1000:.0f} ms" if s < 1 else f"{sign}{s:.1f} s"


def evaluate(o):
    """The single most truthful state, checked in priority order:

    1. the fix in progress (busy)                -- explains everything else
    2. ROS unusable on this laptop (red)
    3. not paired with a robot (grey)
    4. no scan yet, just started (grey)
    5. no scan for STALE seconds (grey)          -- robot ROS down / offline
    6. clocks >= WARN apart (red)
    7. clocks >= GOOD apart (amber)
    8. clocks agree, data arriving late (amber)  -- Wi-Fi delay, same errors
    9. clocks agree, laptop not NTP-synced (amber) -- will jump when it syncs
    10. ready (green)
    """
    if o.action:
        return State("time-busy", o.action, o.action, False)
    if o.ros_error:
        return State("time-red", "ROS unavailable", o.ros_error, False)
    if not o.paired:
        return State("time-grey", "No robot paired", "This laptop isn't in a group with a robot", False)
    if o.offset is None:
        if not o.last_scan and o.now - o.started < STALE:
            return State("time-grey", "Waiting for robot…", "Waiting for the robot's first scan", False)
        since = f"for {o.now - o.last_scan:.0f} s" if o.last_scan else "yet"
        return State("time-grey", "Robot: no data",
                     f"No scans from the robot {since}: is its ROS running?", False)
    robot = -o.offset  # show the robot relative to this laptop
    label = f"Robot {fmt(robot)}"
    if abs(o.offset) >= WARN:
        return State("time-red", label, f"Robot clock {fmt(robot)} from this laptop: OUT OF SYNC", False)
    if abs(o.offset) >= GOOD:
        return State("time-amber", label, f"Robot clock {fmt(robot)} from this laptop: slightly off", False)
    if o.latency is not None and o.latency >= SLOW:
        return State("time-amber", f"{label} · slow",
                     f"Clocks agree, but robot data arrives {o.latency * 1000:.0f} ms late (Wi-Fi?)", False)
    if not o.laptop_synced:
        return State("time-amber", f"{label} · laptop unsynced",
                     "Clocks agree, but this laptop isn't synced yet and will jump when it is", False)
    return State("time-green", label, f"Robot clock {fmt(robot)} from this laptop: in sync", True)


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
    except Exception as e:
        tracker.error = f"ROS unavailable on this laptop: {e}"


class Indicator:
    def __init__(self, tracker, Gtk, GLib, AppIndicator):
        self.Gtk, self.GLib = Gtk, GLib
        self.tracker = tracker
        self.started = time.time()
        self.paired = bool(os.environ.get("ROS_DOMAIN_ID"))
        self.ntp = (False, "")
        self.fix = None  # fix progress: {"phase", "since", "requested", "laptop"}
        self.note = ""  # outcome of the last action, shown in the menu

        self.ind = AppIndicator.Indicator.new_with_path(
            "openrobotfleet-time", "time-grey", AppIndicator.IndicatorCategory.SYSTEM_SERVICES, ICON_DIR)
        self.ind.set_status(AppIndicator.IndicatorStatus.ACTIVE)
        self.ind.set_title("Robot clock sync")

        menu = Gtk.Menu()
        self.robot_item = self.info(menu)
        self.laptop_item = self.info(menu)
        self.scan_item = self.info(menu)
        menu.append(Gtk.SeparatorMenuItem())
        self.fix_robot = Gtk.MenuItem(label="")
        self.fix_robot.connect("activate", lambda _: self.start_fix())
        menu.append(self.fix_robot)
        self.fix_warning = self.info(menu)
        self.fix_warning.set_label("⚠ Also stops nav2, SLAM, RViz and Gazebo on this laptop")
        self.note_item = self.info(menu)
        menu.show_all()
        self.ind.set_menu(menu)

        self.refresh_ntp()
        GLib.timeout_add_seconds(10, self.refresh_ntp)
        self.update()
        GLib.timeout_add(500, self.update)

    def info(self, menu):
        item = self.Gtk.MenuItem(label="")
        item.set_sensitive(False)
        menu.append(item)
        return item

    def refresh_ntp(self):
        threading.Thread(target=lambda: setattr(self, "ntp", laptop_ntp()), daemon=True).start()
        return True

    def start_fix(self):
        """The only action: stops every ROS process on this laptop (leftover
        nodes from an earlier launch break nav2 in ways a robot restart never
        fixes), resyncs this laptop's clock and the robot's, then restarts the
        robot's ROS, so both end up on the lab time server's time and nav2 is
        relaunched from a clean slate. There is deliberately no laptop-only
        resync: jumping the laptop's clock alone breaks a running nav2 session
        without fixing the robot."""
        self.fix, self.note = {"phase": "cleaning-laptop", "since": time.time(), "requested": time.time(),
                               "laptop": ""}, ""

        def run():
            laptop = ask_agent("clean-laptop")
            self.tracker.reset()  # the laptop clock may have just jumped
            self.ntp = laptop_ntp()
            if laptop != "ok":
                self.fix["laptop"] = "This laptop: " + laptop.removeprefix("error: ")
            self.fix.update(phase="requesting")
            reply = ask_agent("fix-robot")
            if reply == "ok":
                self.fix.update(phase="stopping", since=time.time())
            else:
                self.end_fix(reply.removeprefix("error: "))

        threading.Thread(target=run, daemon=True).start()

    def end_fix(self, note):
        laptop = self.fix.get("laptop", "") if self.fix else ""
        self.fix, self.note = None, "\n".join(n for n in (laptop, note) if n)

    def advance_fix(self, now, last_scan):
        """Follows the robot through its restart; returns the busy message."""
        f = self.fix
        elapsed = now - f["requested"]
        if f["phase"] == "cleaning-laptop":
            return "Stopping ROS and syncing the clock on this laptop…"
        if f["phase"] == "requesting":
            return "Asking the robot to restart…"
        if f["phase"] == "stopping":
            if not last_scan or now - last_scan >= STOP_GAP:
                f.update(phase="starting", since=now)
            elif elapsed > STOP_TIMEOUT:
                self.end_fix("The robot didn't restart its ROS. Is it online?")
                return ""
            else:
                return "Robot restarting ROS…"
        if f["phase"] == "starting":
            if last_scan > f["since"]:
                self.tracker.reset()  # measure only the restarted robot
                f.update(phase="settling", since=last_scan)
            elif elapsed > START_TIMEOUT:
                self.end_fix("The robot's ROS didn't come back. Check it, or power-cycle it.")
                return ""
            else:
                return "Robot starting ROS…"
        if f["phase"] == "settling":
            if not last_scan or now - last_scan >= STOP_GAP:  # restarted again
                f.update(phase="starting", since=now)
                return "Robot starting ROS…"
            if now - f["since"] < SETTLE:
                return "Robot checking its clock…"
            self.end_fix("Robot is back: relaunch nav2/SLAM on this laptop")
        return ""

    def update(self):
        now = time.time()
        offset, latency, last = self.tracker.snapshot(now)
        action = self.advance_fix(now, last) if self.fix else ""
        synced, server = self.ntp
        s = evaluate(Observed(now, self.started, self.tracker.error or "", self.paired, offset, latency,
                              last, synced, server, action))

        self.ind.set_icon_full(s.icon, s.label)
        self.ind.set_label(s.label, "Robot −888 ms · laptop unsynced")
        self.robot_item.set_label(s.robot)
        self.laptop_item.set_label(
            f"This laptop: synced to {server}" if synced else f"This laptop: NOT synced ({server or 'no server'})")
        if last:
            delay = f", typical delay {latency * 1000:.0f} ms" if latency is not None else ""
            self.scan_item.set_label(f"Last robot scan {now - last:.1f} s ago{delay}")
        else:
            self.scan_item.set_label("No robot scan received yet")

        busy = bool(action)
        self.fix_robot.set_sensitive(not busy)
        if s.ok or busy:
            self.fix_robot.set_label("Restart ROS on this laptop and the robot, resyncing both clocks")
        else:
            self.fix_robot.set_label("Fix: restart ROS on this laptop and the robot, resyncing both clocks")
        self.fix_warning.set_visible(not busy)
        note = self.note or ("" if s.ok or busy else "Not ready: try the fix above, then relaunch nav2/SLAM")
        self.note_item.set_label(note)
        self.note_item.set_visible(bool(note))
        return True


def main():
    import gi

    gi.require_version("Gtk", "3.0")
    gi.require_version("AyatanaAppIndicator3", "0.1")
    from gi.repository import AyatanaAppIndicator3 as AppIndicator
    from gi.repository import GLib, Gtk

    tracker = ScanOffset()
    threading.Thread(target=spin_ros, args=(tracker,), daemon=True).start()
    Indicator(tracker, Gtk, GLib, AppIndicator)
    for sig in (signal.SIGTERM, signal.SIGINT):
        GLib.unix_signal_add(GLib.PRIORITY_DEFAULT, sig, Gtk.main_quit)
    Gtk.main()


if __name__ == "__main__":
    main()
