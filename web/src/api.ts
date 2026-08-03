import {
  Robot,
  Scenario,
  CommandRequest,
  InstallAgentPayload,
  Job,
  InstallConfig,
  InstallDefaultsResponse,
  DiscoveryCandidate,
  GoldenImageConfig,
  Group,
} from './types';

const JSON_HEADERS = {
  'Content-Type': 'application/json',
};

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, init);
  if (response.status === 401) {
    window.location.href = "/login";
    throw new Error("Unauthorized");
  }
  if (!response.ok) {
    const text = await response.text();
    let message = text;
    try {
      const json = JSON.parse(text);
      if (json.error) {
        message = json.error;
      }
    } catch (e) {
      // ignore
    }
    throw new Error(message);
  }
  if (response.status === 204) {
    return undefined as T;
  }
  const contentType = response.headers.get('content-type') || '';
  if (contentType.includes('application/json')) {
    return (await response.json()) as T;
  }
  return undefined as T;
}

export function getRobots(): Promise<Robot[]> {
  return request<Robot[]>('/api/robots');
}

export function getRobot(id: number | string): Promise<Robot> {
  return request<Robot>(`/api/robots/${id}`);
}

export function updateRobotName(id: number | string, name: string): Promise<Robot> {
  return request<Robot>(`/api/robots/${id}/name`, {
    method: 'PUT',
    headers: JSON_HEADERS,
    body: JSON.stringify({ name }),
  });
}

export function sendCommand(
  robotId: number | string,
  command: CommandRequest,
): Promise<void> {
  return request<void>(`/api/robots/${robotId}/command`, {
    method: 'POST',
    headers: JSON_HEADERS,
    body: JSON.stringify(command),
  });
}

export function broadcastCommand(command: CommandRequest): Promise<void> {
  return request<void>('/api/robots/command/broadcast', {
    method: 'POST',
    headers: JSON_HEADERS,
    body: JSON.stringify(command),
  });
}

export function getJobs(robotId?: number | string): Promise<Job[]> {
  const query = robotId ? `?robot=${robotId}` : '';
  return request<Job[]>(`/api/jobs${query}`);
}

export function getScenarios(): Promise<Scenario[]> {
  return request<Scenario[]>('/api/scenarios');
}

export function getScenario(id: number | string): Promise<Scenario> {
  return request<Scenario>(`/api/scenarios/${id}`);
}

export type ScenarioPayload = Omit<Scenario, 'id'>;

export interface ApplyScenarioPayload {
  robot_ids: number[];
}

export interface ApplyScenarioResponse {
  jobs: Job[];
}

export function createScenario(payload: ScenarioPayload): Promise<Scenario> {
  return request<Scenario>('/api/scenarios', {
    method: 'POST',
    headers: JSON_HEADERS,
    body: JSON.stringify(payload),
  });
}

export function updateScenario(
  id: number | string,
  payload: ScenarioPayload,
): Promise<Scenario> {
  return request<Scenario>(`/api/scenarios/${id}` , {
    method: 'PUT',
    headers: JSON_HEADERS,
    body: JSON.stringify(payload),
  });
}

export function deleteScenario(id: number | string): Promise<void> {
  return request<void>(`/api/scenarios/${id}`, {
    method: 'DELETE',
  });
}

export function applyScenario(
  id: number | string,
  payload: ApplyScenarioPayload,
): Promise<ApplyScenarioResponse> {
  return request<ApplyScenarioResponse>(`/api/scenarios/${id}/apply`, {
    method: 'POST',
    headers: JSON_HEADERS,
    body: JSON.stringify(payload),
  });
}

export function installAgent(payload: InstallAgentPayload): Promise<Robot> {
  return request<Robot>('/api/install-agent', {
    method: 'POST',
    headers: JSON_HEADERS,
    body: JSON.stringify(payload),
  });
}

export function saveInstallConfig(
  robotId: number | string,
  payload: InstallConfig,
): Promise<Robot> {
  return request<Robot>(`/api/robots/${robotId}/install-config`, {
    method: 'PUT',
    headers: JSON_HEADERS,
    body: JSON.stringify(payload),
  });
}

export function getInstallDefaults(): Promise<InstallDefaultsResponse> {
  return request<InstallDefaultsResponse>('/api/settings/install-defaults');
}

export function updateInstallDefaults(
  payload: InstallConfig,
): Promise<InstallDefaultsResponse> {
  return request<InstallDefaultsResponse>('/api/settings/install-defaults', {
    method: 'PUT',
    headers: JSON_HEADERS,
    body: JSON.stringify(payload),
  });
}

export function updateRobotTags(
  robotId: number | string,
  tags: string[],
): Promise<Robot> {
  return request<Robot>(`/api/robots/${robotId}/tags`, {
    method: 'PUT',
    headers: JSON_HEADERS,
    body: JSON.stringify({ tags }),
  });
}

export function scanNetwork(): Promise<DiscoveryCandidate[]> {
  return request<DiscoveryCandidate[]>('/api/discovery/scan', {
    method: 'POST',
  });
}

export interface SemesterRequest {
  robot_ids: number[];
  reinstall: boolean;
  reset_logs: boolean;
  update_repo: boolean;
  run_self_test: boolean;
  repo_config: {
    repo: string;
    branch: string;
    path: string;
  };
  apply_scenarios?: boolean;
  scenario_ids?: number[];
  factory_reset?: boolean;
}

export function startSemesterBatch(req: SemesterRequest): Promise<void> {
  return request<void>('/api/semester/start', {
    method: 'POST',
    headers: JSON_HEADERS,
    body: JSON.stringify(req),
  });
}

export interface SemesterStatus {
  active: boolean;
  total: number;
  completed: number;
  robots: Record<number, string>;
  errors: Record<number, string>;
}

export function getSemesterStatus(): Promise<SemesterStatus> {
  return request<SemesterStatus>('/api/semester/status');
}

export function getGoldenImageConfig(): Promise<{ config: GoldenImageConfig }> {
  return request<{ config: GoldenImageConfig }>('/api/golden-image');
}

export function saveGoldenImageConfig(config: GoldenImageConfig): Promise<{ config: GoldenImageConfig }> {
  return request<{ config: GoldenImageConfig }>('/api/golden-image', {
    method: 'PUT',
    headers: JSON_HEADERS,
    body: JSON.stringify(config),
  });
}

export function buildGoldenImage(): Promise<{ status: string }> {
  return request<{ status: string }>('/api/golden-image/build', {
    method: 'POST',
    headers: JSON_HEADERS,
  });
}

export function getBuildStatus(): Promise<{ status: string; error?: string; progress?: number; step?: string; logs?: string[]; image_name?: string }> {
  return request('/api/golden-image/status');
}

export interface BuildCacheEntry {
  image_name: string;
  robot_model: string;
  ros_version: string;
  overlay_enabled: boolean;
  completed_stage: number;
  total_stages: number;
  updated_at: string;
  size_bytes: number;
}

export function getGoldenImageBuildCache(): Promise<{ entries: BuildCacheEntry[] }> {
  return request('/api/golden-image/cache');
}

export function clearGoldenImageBuildCache(): Promise<{ status: string }> {
  return request('/api/golden-image/cache', { method: 'DELETE' });
}

export interface SystemConfig {
  demo_mode: boolean;
}

export function getSystemConfig(): Promise<SystemConfig> {
  return request<SystemConfig>('/api/settings/system');
}

export function deleteRobot(id: number | string): Promise<void> {
  return request<void>(`/api/robots/${id}`, {
    method: 'DELETE',
  });
}

export function identifyAll(): Promise<Record<number, string>> {
  return request<Record<number, string>>('/api/robots/identify-all', {
    method: 'POST',
    headers: JSON_HEADERS,
  });
}

export type GroupPayload = Omit<Group, 'id'>;

export interface ApplyGroupResult {
  group?: Group;
  applied: string[];
  skipped: string[];
}

export function getGroups(): Promise<Group[]> {
  return request<Group[]>('/api/groups');
}

export function getGroup(id: number | string): Promise<Group> {
  return request<Group>(`/api/groups/${id}`);
}

export function createGroup(payload: GroupPayload): Promise<ApplyGroupResult> {
  return request<ApplyGroupResult>('/api/groups', {
    method: 'POST',
    headers: JSON_HEADERS,
    body: JSON.stringify(payload),
  });
}

export function updateGroup(id: number | string, payload: GroupPayload): Promise<ApplyGroupResult> {
  return request<ApplyGroupResult>(`/api/groups/${id}`, {
    method: 'PUT',
    headers: JSON_HEADERS,
    body: JSON.stringify(payload),
  });
}

export function deleteGroup(id: number | string): Promise<void> {
  return request<void>(`/api/groups/${id}`, {
    method: 'DELETE',
  });
}

export function applyGroup(id: number | string): Promise<ApplyGroupResult> {
  return request<ApplyGroupResult>(`/api/groups/${id}/apply`, {
    method: 'POST',
  });
}

export function downloadRvizLauncher(): void {
  window.location.href = '/api/groups/rviz-launcher';
}
