import React from 'react';
import { Navigate, Outlet } from 'react-router-dom';
import { useAuth, type PanelUserPermissions } from '../context/AuthContext';

interface ProtectedRouteProps {
    requiredPermission?: keyof PanelUserPermissions;
}

export const ProtectedRoute: React.FC<ProtectedRouteProps> = ({ requiredPermission }) => {
    const { isAuthenticated, permissions } = useAuth();

    if (!isAuthenticated) {
        return <Navigate to="/login" replace />;
    }

    if (requiredPermission && permissions && !permissions[requiredPermission]) {
        return <Navigate to="/" replace />;
    }

    return <Outlet />;
};
