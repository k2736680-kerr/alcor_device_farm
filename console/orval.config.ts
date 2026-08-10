import { defineConfig } from 'orval'

export default defineConfig({
  deviceFarm: {
    input: '../openapi/device-farm-v1.yaml',
    hooks: {
      afterAllFilesWrite: 'node ./scripts/trim-generated.mjs',
    },
    output: {
      target: './src/api/generated/device-farm.ts',
      schemas: './src/api/generated/models',
      client: 'react-query',
      httpClient: 'fetch',
      mode: 'split',
      clean: true,
      override: {
        mutator: {
          path: './src/api/fetcher.ts',
          name: 'deviceFarmFetch',
        },
      },
    },
  },
})
