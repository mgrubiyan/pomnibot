import createClient, { type ClientOptions } from 'openapi-fetch'
import type { paths } from './schema'

export type * from './schema'
export { createClient }

export const createApiClient = (options?: ClientOptions) =>
  createClient<paths>({ baseUrl: '/api', ...options })
