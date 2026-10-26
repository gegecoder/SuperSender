// 根目录 app.config.json 的前端类型声明（经 Vite 别名 @app-config 导入）
declare module '@app-config' {
  export interface AppConfigAuthor {
    name: string
    email: string
    blog: string
  }
  export interface AppConfigUpdate {
    apiUrl: string
    releasePageBase: string
  }
  export interface AppConfigBranding {
    brandSub: string
    welcomeTitle: string
    welcomeSub: string
  }
  const appConfig: {
    name: string
    description: string
    version: string
    branding: AppConfigBranding
    author: AppConfigAuthor
    update: AppConfigUpdate
  }
  export default appConfig
}
