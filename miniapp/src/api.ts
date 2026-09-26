import { createApiClient } from '../contracts';
import { initData } from './max/bridge';

/**
 * The launch data travels in this header, so the backend can check its
 * signature and tell who is asking (https://dev.max.ru/docs/webapps/validation).
 * The name is a contract with the backend: change it on both sides at once.
 */
const INIT_DATA_HEADER = 'X-Max-Init-Data';

/** The one client for every request; outside MAX it goes without the header. */
export const api = createApiClient();

// Read on every request rather than once on import: that way the header
// does not depend on the bridge script having run before this module.
api.use({
    onRequest({ request }) {
        const data = initData();
        if (data) {
            request.headers.set(INIT_DATA_HEADER, data);
        }
        return request;
    },
});
