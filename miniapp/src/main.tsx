import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import '@maxhub/max-ui/dist/styles.css';
import App from './App';
import { MaxUIRoot } from './components/MaxUIRoot';
import { ProfileGate } from './components/ProfileGate';
import { telegramApp } from './platform';

const telegram = telegramApp();
if (telegram) { telegram.ready(); telegram.expand(); }
else window.WebApp?.ready?.();

createRoot(document.getElementById('root')!).render(
    <StrictMode>
        <MaxUIRoot>
            <ProfileGate><App /></ProfileGate>
        </MaxUIRoot>
    </StrictMode>
);
