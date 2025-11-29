import { BrowserRouter, Routes, Route } from "react-router-dom";
import { Layout } from "./Layout";
import { Login } from "./pages/Login";
import { AuthGuard } from "./components/AuthGuard";
import { Dashboard } from "./pages/Dashboard";
import { Robots } from "./pages/Robots";
import { Laptops } from "./pages/Laptops";
import { RobotDetail } from "./pages/RobotDetail";
import { Scenarios } from "./pages/Scenarios";
import { Settings } from "./pages/Settings";
import { InstallAgent } from "./pages/InstallAgent";
import { ScenarioEditor } from "./pages/ScenarioEditor";
import { Discovery } from "./pages/Discovery";
import { SemesterWizard } from "./pages/SemesterWizard";
import { GoldenImage } from "./pages/GoldenImage";

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<Login />} />

        <Route element={<AuthGuard />}>
          <Route path="/" element={<Layout />}>
            <Route index element={<Dashboard />} />
            <Route path="robots" element={<Robots />} />
            <Route path="laptops" element={<Laptops />} />
            <Route path="robots/:id" element={<RobotDetail />} />
            <Route path="discovery" element={<Discovery />} />
            <Route path="semester-wizard" element={<SemesterWizard />} />
            <Route path="install" element={<InstallAgent />} />
            <Route path="scenarios" element={<Scenarios />} />
            <Route path="scenarios/new" element={<ScenarioEditor />} />
            <Route path="scenarios/:id" element={<ScenarioEditor />} />
            <Route path="settings" element={<Settings />} />
            <Route path="golden-image" element={<GoldenImage />} />
          </Route>
        </Route>
      </Routes>
    </BrowserRouter>
  );
}
