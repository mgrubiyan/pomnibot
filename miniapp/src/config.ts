import { createAppConfig, type AppEnvironment } from '../config'

declare const __APP_ENV__: AppEnvironment

export const config = createAppConfig(__APP_ENV__)
