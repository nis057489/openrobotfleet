import { Robot } from '../types';

export function getRobotMood(robot: Robot): string {
  // Check if offline (last seen > 5 minutes ago)
  if (robot.last_seen) {
    const lastSeen = new Date(robot.last_seen).getTime();
    const now = new Date().getTime();
    if (now - lastSeen > 5 * 60 * 1000) {
      return '🙈'; // Disappeared
    }
  } else {
      return '👶'; // New
  }

  // Check status
  if (robot.status === 'offline') return '💤'; // Sleeping
  if (robot.status === 'busy') return '😰'; // Stressed
  if (robot.status === 'error') return '😵'; // Confused/Error

  // Default
  return '😎'; // Idle
}
