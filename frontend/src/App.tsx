import { BrowserRouter as Router, Routes, Route, Navigate } from 'react-router-dom';
import { ToastProvider } from './context/ToastContext';
import Dashboard from './components/Dashboard';
import UserManagement from './components/UserManagement';
import WireGuard from './components/WireGuard';
import Settings from './components/Settings';
import LogViewer from './components/LogViewer';
import RawConfig from './components/RawConfig';
import { Login } from './components/Login';
import { ProtectedRoute } from './components/ProtectedRoute';
import { AuthProvider } from './context/AuthContext';
import { Layout } from './components/Layout';

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
