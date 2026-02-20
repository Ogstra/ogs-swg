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
                                <Route path="/users" element={<UserManagement />} />
                                <Route path="/wireguard" element={<WireGuard />} />
                                <Route path="/logs" element={<LogViewer />} />
                                <Route path="/raw-config" element={<RawConfig />} />
                                <Route path="/settings" element={<Settings />} />
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
