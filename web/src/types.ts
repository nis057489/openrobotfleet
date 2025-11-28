export interface Robot {
  id: number;
  name: string;
  agent_id: string;
  ip?: string;
  last_seen?: string;
  status?: string;
  notes?: string;
}

export interface Scenario {
  id: number;
  name: string;
  description?: string;
  config_yaml: string;
}

export interface CommandRequest {
  type: string;
  data: Record<string, any>;
}

export interface InstallAgentPayload {
  name: string;
  address: string;
  user: string;
  ssh_key: string;
}
