import { createApiClient } from '../contracts';
import type { Middleware } from 'openapi-fetch';

const authMiddleware: Middleware = {
    async onRequest({ request }) {
        const initData = window.WebApp?.initData;
        if (initData) {
            request.headers.set('X-Init-Data', initData);
        }
        return request;
    },
};

export const api = createApiClient();
api.use(authMiddleware);

export * from '../contracts';
