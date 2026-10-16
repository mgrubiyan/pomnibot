import { createApiClient } from '../contracts';
import { config } from './config';
import type { Middleware } from 'openapi-fetch';

const authMiddleware: Middleware = {
    async onRequest({ request }) {
        const initData = config.maxEnabled && window.WebApp?.initData;
        if (initData) {
            request.headers.set('X-Init-Data', initData);
        }
        return request;
    },
};

export const api = createApiClient({ baseUrl: config.apiBaseUrl });
api.use(authMiddleware);

export * from '../contracts';
