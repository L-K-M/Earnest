import js from '@eslint/js'
import tseslint from 'typescript-eslint'
import hooks from 'eslint-plugin-react-hooks'
export default tseslint.config({ignores: ['src/api.generated.ts', 'dist']}, js.configs.recommended, ...tseslint.configs.recommended, {files: ['src/**/*.{ts,tsx}'], plugins: {'react-hooks': hooks}, rules: hooks.configs.recommended.rules, languageOptions: {globals: {document: 'readonly', window: 'readonly', fetch: 'readonly', localStorage: 'readonly', crypto: 'readonly', URL: 'readonly'}}})
