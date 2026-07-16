const path = require('path')
const root = __dirname

module.exports = {
  apps: [
    {
      name: 'flyaimovie',
      script: path.join(root, '.run/pm2-start.js'),
      cwd: root,
      interpreter: 'node',
      instances: 1,
      exec_mode: 'fork',
      autorestart: true,
      watch: false,
      max_restarts: 100,
      min_uptime: '3s',
      restart_delay: 1000,
      kill_timeout: 5000,
      env: {
        PORT: '8088',
        HOME: process.env.HOME || '/Users/eqzhou',
        NODE_ENV: 'production',
        APP_ENV: process.env.APP_ENV || 'development',
        FLYAIMOVIE_BIN: process.env.FLYAIMOVIE_BIN || path.join(root, '.run/flyaimovie-server'),
        CONFIG_PATH: process.env.CONFIG_PATH || path.join(root, 'configs/config.yaml'),
        WEBHOOK_SECRET: process.env.WEBHOOK_SECRET || '',
        AI_CONFIG_ENCRYPTION_KEY: process.env.AI_CONFIG_ENCRYPTION_KEY || '',
        SMTP_HOST: process.env.SMTP_HOST || '',
        SMTP_PORT: process.env.SMTP_PORT || '',
        SMTP_USERNAME: process.env.SMTP_USERNAME || '',
        SMTP_PASSWORD: process.env.SMTP_PASSWORD || '',
        EMAIL_FROM: process.env.EMAIL_FROM || '',
        PASSWORD_RESET_URL_BASE: process.env.PASSWORD_RESET_URL_BASE || '',
      },
      out_file: path.join(root, 'logs/pm2-out.log'),
      error_file: path.join(root, 'logs/pm2-error.log'),
      merge_logs: true,
      time: true,
    },
  ],
}
