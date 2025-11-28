import {
  Robot,
  Scenario,
  CommandRequest,
  InstallAgentPayload,
  Job,
} from './types';

const JSON_HEADERS = {
  'Content-Type': 'application/json',
};

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, init);
  if (!response.ok) {
    const message = await response.text();
    throw new Error(
      `Request failed ${response.status} ${response.statusText}: ${message}`,
    );
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

export function installAgent(payload: InstallAgentPayload): Promise<void> {
  return request<void>('/api/install-agent', {
    method: 'POST',
    headers: JSON_HEADERS,
    body: JSON.stringify(payload),
  });
}
