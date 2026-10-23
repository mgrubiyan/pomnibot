import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import '@maxhub/max-ui/dist/styles.css';
import './global.css';
import App from './App';
import { MaxUIRoot } from './components/MaxUIRoot';
import { ProfileGate } from './components/ProfileGate';
import { telegramApp } from './platform';

const telegram = telegramApp();
if (telegram) {
    telegram.ready();
    telegram.expand();
    // A pull down at the top of a scrolled list would otherwise close the app.
    // Older clients may throw on it; the app must start anyway.
    try { telegram.disableVerticalSwipes?.(); } catch { /* unsupported */ }
} else {
    window.WebApp?.ready?.();
}

createRoot(document.getElementById('root')!).render(
    <StrictMode>
        <MaxUIRoot>
            <ProfileGate><App /></ProfileGate>
        </MaxUIRoot>
    </StrictMode>
);
