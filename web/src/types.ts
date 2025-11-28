export interface Robot {
  id: number;
  name: string;
  type: string;
  agent_id: string;
  ip?: string;
  last_seen?: string;
  status?: string;
  notes?: string;
  last_scenario?: ScenarioRef;
  install_config?: InstallConfig;
  tags?: string[];
}

export interface ScenarioRef {
  id: number;
  name: string;
}

export interface Scenario {
  id: number;
  name: string;
  description?: string;
  config_yaml: string;
}

export interface Job {
  id: number;
  type: string;
  target_robot: string;
  payload_json: string;
  status: string;
  created_at?: string;
  updated_at?: string;
}

export interface CommandRequest {
  type: string;
  data: Record<string, any>;
}

export interface InstallAgentPayload {
  name: string;
  type: string;
  address: string;
  user: string;
  ssh_key: string;
}

export interface InstallConfig {
  address: string;
  user: string;
  ssh_key: string;
}

export interface InstallDefaultsResponse {
  install_config?: InstallConfig | null;
}

export interface DiscoveryCandidate {
  ip: string;
  port: number;
  mac?: string;
  manufacturer?: string;
  status?: string;
}

export interface SemesterStatus {
  active: boolean;
  total: number;
  completed: number;
  robots: Record<string, string>;
  errors: Record<string, string>;
}
