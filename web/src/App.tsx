import { BrowserRouter, Routes, Route } from "react-router-dom";
import { Layout } from "./Layout";
import { Dashboard } from "./pages/Dashboard";
import { Robots } from "./pages/Robots";
import { RobotDetail } from "./pages/RobotDetail";
import { Scenarios } from "./pages/Scenarios";
import { Settings } from "./pages/Settings";
import { InstallAgent } from "./pages/InstallAgent";
import { ScenarioEditor } from "./pages/ScenarioEditor";

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<Layout />}>
          <Route index element={<Dashboard />} />
          <Route path="robots" element={<Robots />} />
          <Route path="robots/:id" element={<RobotDetail />} />
          <Route path="install" element={<InstallAgent />} />
          <Route path="scenarios" element={<Scenarios />} />
          <Route path="scenarios/new" element={<ScenarioEditor />} />
          <Route path="scenarios/:id" element={<ScenarioEditor />} />
          <Route path="settings" element={<Settings />} />
        </Route>
      </Routes>
    </BrowserRouter>
  );
}
