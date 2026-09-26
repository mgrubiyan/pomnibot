import { createApiClient } from '../contracts';
import { initData } from './max/bridge';

/**
 * The launch data travels in this header, so the backend can check its
 * signature and tell who is asking (https://dev.max.ru/docs/webapps/validation).
 * The name is a contract with the backend: change it on both sides at once.
 */
const INIT_DATA_HEADER = 'X-Max-Init-Data';

const launchData = initData();

/** The one client for every request; outside MAX it goes without the header. */
export const api = createApiClient(
    launchData ? { headers: { [INIT_DATA_HEADER]: launchData } } : undefined,
);
