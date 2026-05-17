import React, { createContext, useContext, useState, useEffect } from 'react';

export interface PanelUserPermissions {
    can_read_users: boolean;
    can_write_users: boolean;
    can_read_wireguard: boolean;
    can_write_wireguard: boolean;
    can_read_config: boolean;
    can_write_config: boolean;
    can_read_settings: boolean;
    can_write_settings: boolean;
    can_read_panel_users: boolean;
    can_write_panel_users: boolean;
    can_read_logs: boolean;
    can_read_logs_censored: boolean;
}

interface AuthContextType {
    isAuthenticated: boolean;
    token: string | null;
    permissions: PanelUserPermissions | null;
    login: (username: string, pass: string) => Promise<void>;
    logout: () => void;
}

const AuthContext = createContext<AuthContextType | null>(null);

const PERMISSIONS_KEY = 'permissions';
const API_KEY_KEY = 'api_key';
const DASHBOARD_PREFS_KEY = 'dashboard:prefs:v1';
const DASHBOARD_SNAPSHOT_PREFIX = 'dashboard:snapshot:v1:';

// DEPRECATED: granular permission gating is disabled.
// The PanelUserPermissions interface and the per-field checks in UI components
// are preserved for future reimplementation. Until then every authenticated user
// receives all-true permissions so no nav item is hidden and no button is greyed out.
// TODO(reimplement): restore per-permission UI guards when the permission model
// is redesigned. Re-enable the original body below and remove this stub.
const normalizePermissions = (_raw: any): PanelUserPermissions => ({
    // DEPRECATED stub — all permissions granted unconditionally.
    can_read_users: true,
    can_write_users: true,
    can_read_wireguard: true,
    can_write_wireguard: true,
    can_read_config: true,
    can_write_config: true,
    can_read_settings: true,
    can_write_settings: true,
    can_read_panel_users: true,
    can_write_panel_users: true,
    can_read_logs: true,
    can_read_logs_censored: true,
    /* original (disabled):
    can_read_users: !!raw?.can_read_users,
    can_write_users: !!raw?.can_write_users,
    can_read_wireguard: !!raw?.can_read_wireguard,
    can_write_wireguard: !!raw?.can_write_wireguard,
    can_read_config: !!raw?.can_read_config,
    can_write_config: !!raw?.can_write_config,
    can_read_settings: !!raw?.can_read_settings,
    can_write_settings: !!raw?.can_write_settings,
    can_read_panel_users: !!raw?.can_read_panel_users,
    can_write_panel_users: !!raw?.can_write_panel_users,
    can_read_logs: !!raw?.can_read_logs,
    can_read_logs_censored: !!raw?.can_read_logs_censored,
    */
});

const clearDashboardStartupCache = () => {
    localStorage.removeItem(DASHBOARD_PREFS_KEY);
    for (let i = localStorage.length - 1; i >= 0; i -= 1) {
        const key = localStorage.key(i);
        if (key?.startsWith(DASHBOARD_SNAPSHOT_PREFIX)) {
            localStorage.removeItem(key);
        }
    }
};

export const AuthProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
    const [token, setToken] = useState<string | null>(localStorage.getItem('token'));
    const [apiKey, setApiKey] = useState<string | null>(localStorage.getItem(API_KEY_KEY));
    const [permissions, setPermissions] = useState<PanelUserPermissions | null>(() => {
        const raw = localStorage.getItem(PERMISSIONS_KEY);
        if (raw) {
            try { return normalizePermissions(JSON.parse(raw)); } catch { return null; }
        }
        return null;
    });
    const isAuthenticated = !!token || !!apiKey;

    useEffect(() => {
        if (token) {
            localStorage.setItem('token', token);
        } else {
            localStorage.removeItem('token');
        }
    }, [token]);

    useEffect(() => {
        if (apiKey) {
            localStorage.setItem(API_KEY_KEY, apiKey);
        } else {
            localStorage.removeItem(API_KEY_KEY);
        }
    }, [apiKey]);

    useEffect(() => {
        if (permissions) {
            localStorage.setItem(PERMISSIONS_KEY, JSON.stringify(permissions));
        } else {
            localStorage.removeItem(PERMISSIONS_KEY);
        }
    }, [permissions]);

    useEffect(() => {
        const handleUnauthorized = () => {
            // In demo mode the api_key is injected by the autologin script on
            // every page load and never expires. A 401 here means the server
            // was briefly unavailable (e.g. container restart). Reload the page
            // so the autologin script re-runs and auth is restored automatically.
            if (localStorage.getItem('demo_mode') === '1') {
                window.location.reload();
                return;
            }
            logout();
        };
        window.addEventListener('auth:unauthorized', handleUnauthorized);
        return () => {
            window.removeEventListener('auth:unauthorized', handleUnauthorized);
        };
    }, []);

    const login = async (username: string, pass: string) => {
        const res = await fetch('/api/login', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ username, password: pass }),
        });

        if (!res.ok) {
            throw new Error('Login failed');
        }

        const data = await res.json();
        clearDashboardStartupCache();
        setApiKey(null);
        setToken(data.token);
        setPermissions(data.permissions ? normalizePermissions(data.permissions) : null);
    };

    const logout = () => {
        clearDashboardStartupCache();
        setToken(null);
        setApiKey(null);
        setPermissions(null);
        localStorage.removeItem('token');
        localStorage.removeItem(API_KEY_KEY);
        localStorage.removeItem(PERMISSIONS_KEY);
    };

    return (
        <AuthContext.Provider value={{ isAuthenticated, token, permissions, login, logout }}>
            {children}
        </AuthContext.Provider>
    );
};

export const useAuth = () => {
    const context = useContext(AuthContext);
    if (!context) {
        throw new Error('useAuth must be used within an AuthProvider');
    }
    return context;
};
