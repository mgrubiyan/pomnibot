import { createApiClient } from '../contracts';
import { config } from './config';
import type { Middleware } from 'openapi-fetch';
import { miniAppAuthorization } from './platform';
import { noteResponseStatus } from './utils/signIn';

const authMiddleware: Middleware = {
    async onRequest({ request }) {
        const authorization = miniAppAuthorization(window);
        if (authorization) {
            request.headers.set('Authorization', authorization);
        }
        return request;
    },
    async onResponse({ response }) {
        noteResponseStatus(response.status);
        return response;
    },
};

export const api = createApiClient({ baseUrl: config.apiBaseUrl });
api.use(authMiddleware);

export * from '../contracts';
