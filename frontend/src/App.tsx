import { Suspense, lazy, Component, type ReactNode, type ErrorInfo } from 'react';
import { BrowserRouter as Router, Routes, Route, Navigate } from 'react-router-dom';
import { ToastProvider } from './context/ToastContext';
import { Login } from './pages/Login';
import { ProtectedRoute } from './components/ProtectedRoute';
import { AuthProvider } from './context/AuthContext';
import { Layout } from './layouts/Layout';

const Dashboard = lazy(() => import('./pages/Dashboard'));
const UserManagement = lazy(() => import('./pages/UserManagement'));
const WireGuard = lazy(() => import('./pages/WireGuard'));
const Settings = lazy(() => import('./pages/Settings'));
const LogViewer = lazy(() => import('./pages/LogViewer'));
const RawConfig = lazy(() => import('./pages/RawConfig'));
const Subscriptions = lazy(() => import('./pages/Subscriptions'));

class ChunkErrorBoundary extends Component<{ children: ReactNode }, { errored: boolean }> {
    state = { errored: false }
    static getDerivedStateFromError() { return { errored: true } }
    componentDidCatch(_err: Error, _info: ErrorInfo) { window.location.reload() }
    render() { return this.state.errored ? null : this.props.children }
}

function RouteFallback() {
    return (
        <div className="flex min-h-[240px] items-center justify-center text-sm text-slate-400">
            Loading...
        </div>
    );
}

function App() {
    return (
        <AuthProvider>
            <ToastProvider>
                <Router>
                    <ChunkErrorBoundary>
                    <Suspense fallback={<RouteFallback />}>
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
                                    <Route path="*" element={<Navigate to="/" replace />} />
                                </Route>
                            </Route>
                        </Routes>
                    </Suspense>
                    </ChunkErrorBoundary>
                </Router>
            </ToastProvider>
        </AuthProvider>
    );
}

export default App;
