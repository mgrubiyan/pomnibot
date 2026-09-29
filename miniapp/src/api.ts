import { createApiClient } from '../contracts';
import type { Middleware } from 'openapi-fetch';
import { initData } from './max/bridge';

const authMiddleware: Middleware = {
    async onRequest({ request }) {
        const data = initData();
        if (data) {
            request.headers.set('X-Init-Data', data);
        }
        return request;
    },
};

export const api = createApiClient();
api.use(authMiddleware);

export * from '../contracts';
