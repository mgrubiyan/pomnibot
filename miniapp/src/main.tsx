import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { MaxUI } from '@maxhub/max-ui';
import '@maxhub/max-ui/dist/styles.css';
import App from './App';
import { ready } from './max/bridge';

// Notify MAX messenger that web app is loaded and ready to be displayed
ready();

createRoot(document.getElementById('root')!).render(
    <StrictMode>
        <MaxUI resetBody>
            <App />
        </MaxUI>
    </StrictMode>
);
