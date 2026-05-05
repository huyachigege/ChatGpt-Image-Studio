import { Navigate, Route, Routes } from "react-router-dom";

import AccountsPage from "@/app/accounts/page";
import ImagePage from "@/app/image/page";
import ImageGalleryPage from "@/app/image-gallery/page";
import AppShell from "@/app/layout";
import LoginPage from "@/app/login/page";
import HomePage from "@/app/page";
import RequestsPage from "@/app/requests/page";
import SettingsPage from "@/app/settings/page";
import StartupCheckPage from "@/app/startup-check/page";

export default function App() {
  return (
    <AppShell>
      <Routes>
        <Route path="/" element={<HomePage />} />
        <Route path="/login" element={<LoginPage />} />
        <Route path="/image" element={<Navigate to="/image/history" replace />} />
        <Route path="/image/history" element={<ImagePage />} />
        <Route path="/image/workspace" element={<ImagePage />} />
        <Route path="/image/gallery" element={<ImageGalleryPage />} />
        <Route path="/accounts" element={<AccountsPage />} />
        <Route path="/settings" element={<SettingsPage />} />
        <Route path="/startup-check" element={<StartupCheckPage />} />
        <Route path="/requests" element={<RequestsPage />} />
      </Routes>
    </AppShell>
  );
}
