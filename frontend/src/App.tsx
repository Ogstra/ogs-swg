import { Suspense, lazy } from 'react';
import { BrowserRouter as Router, Routes, Route, Navigate } from 'react-router-dom';
import { ToastProvider } from './context/ToastContext';
import Dashboard from './components/Dashboard';
import { Login } from './components/Login';
import { ProtectedRoute } from './components/ProtectedRoute';
import { AuthProvider } from './context/AuthContext';
import { Layout } from './components/Layout';

const UserManagement = lazy(() => import('./components/UserManagement'));
const WireGuard = lazy(() => import('./components/WireGuard'));
const Settings = lazy(() => import('./components/Settings'));
const LogViewer = lazy(() => import('./components/LogViewer'));
const RawConfig = lazy(() => import('./components/RawConfig'));

const RouteFallback = () => (
    <div className="p-8 text-center text-slate-400">Loading...</div>
);

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
                                    <Suspense fallback={<RouteFallback />}>
                                        <UserManagement />
                                    </Suspense>
                                } />
                                <Route path="/wireguard" element={
                                    <Suspense fallback={<RouteFallback />}>
                                        <WireGuard />
                                    </Suspense>
                                } />
                                <Route path="/logs" element={
                                    <Suspense fallback={<RouteFallback />}>
                                        <LogViewer />
                                    </Suspense>
                                } />
                                <Route path="/raw-config" element={
                                    <Suspense fallback={<RouteFallback />}>
                                        <RawConfig />
                                    </Suspense>
                                } />
                                <Route path="/settings" element={
                                    <Suspense fallback={<RouteFallback />}>
                                        <Settings />
                                    </Suspense>
                                } />
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
