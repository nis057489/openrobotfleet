"""Tests for indicator/time_indicator.py: python3 internal/agent/time_indicator_test.py"""

import os
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "indicator"))
import time_indicator as ti  # noqa: E402


def observed(**kw):
    o = dict(now=100.0, started=0.0, ros_error="", paired=True, offset=0.003, latency=0.004,
             last_scan=99.9, laptop_synced=True, laptop_server="192.168.100.1", action="")
    o.update(kw)
    return ti.Observed(**o)


class EvaluateTest(unittest.TestCase):
    def check(self, icon, label_has, **kw):
        s = ti.evaluate(observed(**kw))
        self.assertEqual(s.icon, icon, s)
        self.assertIn(label_has, s.label)
        return s

    def test_states_in_priority_order(self):
        self.check("time-busy", "restarting", action="Robot restarting ROS…", offset=5.0)
        self.check("time-red", "ROS unavailable", ros_error="ROS unavailable: boom", paired=False)
        self.check("time-grey", "No robot paired", paired=False, offset=5.0)
        self.check("time-grey", "Waiting", offset=None, latency=None, last_scan=0.0, now=3.0)
        s = self.check("time-grey", "no data", offset=None, latency=None, last_scan=0.0, now=30.0)
        self.assertIn("yet", s.robot)
        s = self.check("time-grey", "no data", offset=None, latency=None, last_scan=88.0)
        self.assertIn("for 12 s", s.robot)
        self.check("time-red", "Robot −2.4 s", offset=2.4)  # robot behind
        self.check("time-red", "Robot +250 ms", offset=-0.25)  # robot ahead
        self.check("time-amber", "Robot +80 ms", offset=-0.08)
        self.check("time-amber", "slow", latency=0.3)
        self.check("time-amber", "laptop unsynced", laptop_synced=False)
        s = self.check("time-green", "Robot −3 ms")
        self.assertTrue(s.ok)

    def test_only_green_is_ok(self):
        for kw in [dict(offset=0.1), dict(latency=0.3), dict(laptop_synced=False), dict(offset=None),
                   dict(action="busy")]:
            self.assertFalse(ti.evaluate(observed(**kw)).ok, kw)


class FakeTracker:
    def __init__(self):
        self.resets = 0

    def reset(self):
        self.resets += 1


class FixRobotTest(unittest.TestCase):
    def make(self):
        ind = ti.Indicator.__new__(ti.Indicator)  # skip GTK setup
        ind.tracker, ind.note = FakeTracker(), ""
        ind.fix = {"phase": "stopping", "since": 0.0, "requested": 0.0, "laptop": ""}
        return ind

    def test_laptop_is_synced_first_and_its_failure_reported(self):
        ind = self.make()
        ind.fix.update(phase="syncing-laptop")
        self.assertIn("this laptop", ind.advance_fix(0.5, last_scan=0.4))
        ind.fix.update(phase="stopping", laptop="This laptop's clock didn't sync: no server")
        ind.advance_fix(5.0, last_scan=1.0)
        ind.advance_fix(6.0, last_scan=5.9)
        ind.advance_fix(15.0, last_scan=14.9)
        self.assertIsNone(ind.fix)
        self.assertIn("didn't sync", ind.note)  # reported alongside the robot outcome
        self.assertIn("relaunch", ind.note)

    def test_follows_restart_until_settled(self):
        ind = self.make()
        self.assertIn("restarting", ind.advance_fix(1.0, last_scan=0.9))  # scans still coming
        self.assertIn("starting", ind.advance_fix(5.0, last_scan=2.0))  # 3 s gap: stopped
        self.assertIn("starting", ind.advance_fix(10.0, last_scan=2.0))
        self.assertIn("checking", ind.advance_fix(12.0, last_scan=11.9))  # back
        self.assertEqual(ind.tracker.resets, 1)
        self.assertIn("checking", ind.advance_fix(16.0, last_scan=15.9))
        self.assertIn("starting", ind.advance_fix(19.0, last_scan=16.5))  # restarted again
        self.assertIn("checking", ind.advance_fix(20.0, last_scan=19.9))
        self.assertEqual(ind.advance_fix(28.5, last_scan=28.4), "")  # 8 s of fresh scans
        self.assertIsNone(ind.fix)
        self.assertIn("relaunch", ind.note)

    def test_times_out_if_robot_never_stops_or_returns(self):
        ind = self.make()
        self.assertEqual(ind.advance_fix(50.0, last_scan=49.9), "")
        self.assertIn("didn't restart", ind.note)
        ind = self.make()
        ind.advance_fix(5.0, last_scan=1.0)
        self.assertEqual(ind.advance_fix(200.0, last_scan=1.0), "")
        self.assertIn("didn't come back", ind.note)


class ScanOffsetTest(unittest.TestCase):
    def test_offset_is_min_and_latency_is_median_above_it(self):
        t = ti.ScanOffset()
        for arrival, delay in [(10.0, 2.412), (10.1, 2.403), (10.2, 2.405), (10.3, 2.450), (10.4, 2.404)]:
            t.samples.append((arrival, delay))
        t.last = 10.4
        offset, latency, last = t.snapshot(10.5)
        self.assertAlmostEqual(offset, 2.403)
        self.assertAlmostEqual(latency, 0.002)
        self.assertEqual(t.snapshot(20.0)[:2], (None, None))  # outside the window


if __name__ == "__main__":
    unittest.main()
