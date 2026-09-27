import createClient, { type ClientOptions, type Middleware } from 'openapi-fetch';
import type { paths } from './schema';

export type * from './schema';
export { createClient };

const authMiddleware: Middleware = {
    async onRequest({ request }) {
        const initData = window.WebApp?.initData;
        if (initData) {
            request.headers.set('X-Init-Data', initData);
        }
        return request;
    },
};

export const createApiClient = (options?: ClientOptions) => {
    const client = createClient<paths>({ baseUrl: '/api', ...options });
    client.use(authMiddleware);
    return client;
};

export const api = createApiClient();
