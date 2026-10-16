export type AppEnvironment = 'local' | 'prod'

export function parseEnvironment(value = 'prod'): AppEnvironment {
  const environment = value.trim() || 'prod'
  if (environment !== 'local' && environment !== 'prod') {
    throw new Error('APP_ENV must be local or prod')
  }
  return environment
}

export function createAppConfig(environment: AppEnvironment) {
  return Object.freeze({
    environment,
    apiBaseUrl: '/api',
    maxEnabled: environment === 'prod',
  })
}
