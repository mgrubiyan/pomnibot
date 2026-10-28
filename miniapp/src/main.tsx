import { StrictMode, Suspense } from 'react';
import { createRoot } from 'react-dom/client';
import '@maxhub/max-ui/dist/styles.css';
import './global.css';
import App from './App';
import { StatusScreen } from './components/StatusScreen';
import { MaxUIRoot } from './components/MaxUIRoot';
import { ProfileGate } from './components/ProfileGate';
import { SignInGate } from './components/SignInGate';

window.WebApp?.ready?.();

createRoot(document.getElementById('root')!).render(
    <StrictMode>
        <MaxUIRoot>
            <SignInGate>
                <ProfileGate>
                    <Suspense fallback={<StatusScreen icon={null} title="Открываем экран" text="Одну секунду…" />}>
                        <App />
                    </Suspense>
                </ProfileGate>
            </SignInGate>
        </MaxUIRoot>
    </StrictMode>
);
