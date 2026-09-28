import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { MaxUI } from '@maxhub/max-ui';
import '@maxhub/max-ui/dist/styles.css';
import App from './App';

declare global {
    interface Window {
        WebApp?: {
            ready?: () => void;
            close?: () => void;
            initData?: string;
            [key: string]: unknown;
        };
    }
}

// Notify MAX messenger that web app is loaded and ready to be displayed
window.WebApp?.ready?.();

createRoot(document.getElementById('root')!).render(
    <StrictMode>
        <MaxUI resetBody>
            <App />
        </MaxUI>
    </StrictMode>
);
