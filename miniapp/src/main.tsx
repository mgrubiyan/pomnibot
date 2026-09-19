import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { MaxUI } from '@maxhub/max-ui';
import '@maxhub/max-ui/dist/styles.css';
import App from './App';

createRoot(document.getElementById('root')!).render(
    <StrictMode>
        <MaxUI resetBody>
            <App />
        </MaxUI>
    </StrictMode>
);
