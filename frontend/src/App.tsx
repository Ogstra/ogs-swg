import { BrowserRouter as Router, Routes, Route, Navigate } from 'react-router-dom';
import { ToastProvider } from './context/ToastContext';
import Dashboard from './pages/Dashboard';
import { Login } from './pages/Login';
import { ProtectedRoute } from './components/ProtectedRoute';
import { AuthProvider } from './context/AuthContext';
import { Layout } from './layouts/Layout';

import UserManagement from './pages/UserManagement';
import WireGuard from './pages/WireGuard';
import Settings from './pages/Settings';
import LogViewer from './pages/LogViewer';
import RawConfig from './pages/RawConfig';
import Subscriptions from './pages/Subscriptions';

function App() {
    return (
        <AuthProvider>
            <ToastProvider>
                <Router>
                    <Routes>
                        <Route path="/login" element={<Login />} />

                        <Route element={<ProtectedRoute />}>
                            <Route element={<Layout />}>
                                <Route path="/" element={<Dashboard />} />
                                <Route path="/users" element={
                                    <ProtectedRoute requiredPermission="can_read_users" />
                                }>
                                    <Route index element={<UserManagement />} />
                                </Route>
                                <Route path="/subscriptions" element={
                                    <ProtectedRoute requiredPermission="can_read_users" />
                                }>
                                    <Route index element={<Subscriptions />} />
                                </Route>
                                <Route path="/wireguard" element={
                                    <ProtectedRoute requiredPermission="can_read_wireguard" />
                                }>
                                    <Route index element={<WireGuard />} />
                                </Route>
                                <Route path="/logs" element={
                                    <ProtectedRoute requiredPermission="can_read_logs" />
                                }>
                                    <Route index element={<LogViewer />} />
                                </Route>
                                <Route path="/raw-config" element={
                                    <ProtectedRoute requiredPermission="can_read_config" />
                                }>
                                    <Route index element={<RawConfig />} />
                                </Route>
                                <Route path="/settings" element={
                                    <ProtectedRoute requiredPermission="can_read_settings" />
                                }>
                                    <Route index element={<Settings />} />
                                </Route>
{/* Redirect unknown routes to dashboard */}
                                <Route path="*" element={<Navigate to="/" replace />} />
                            </Route>
                        </Route>
                    </Routes>
                </Router>
            </ToastProvider>
        </AuthProvider>
    );
}

export default App;
