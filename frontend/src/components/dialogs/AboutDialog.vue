<script setup lang="ts">
import { computed, ref } from 'vue'
import { Browser } from '@wailsio/runtime'
import { useUpdate } from '@/composables/useUpdate'
import { i18n } from '@/i18n'
import appConfig from '@app-config'

// 关于弹窗：居中展示 logo、应用名、描述、版本徽标（含检查更新）、作者/博客信息与赞助二维码
// 名称与描述取自根目录 app.config.json，与 Go 端共用同一份配置
const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ 'update:visible': [v: boolean] }>()

const { appVersion, checking, checkStatus, checkMessage, checkForUpdate } = useUpdate()

const appName = appConfig.name

// 作者信息（取自 app.config.json 的 author 段，与 Go 端共用同一份配置）
const AUTHOR_NAME = appConfig.author.name
const AUTHOR_BLOG = appConfig.author.blog

const rewardVisible = ref(true)

const showVerBadge = computed(() => !!appVersion.value)
const updateStateClass = computed(() => checkStatus.value)

function openBlog() {
  Browser.OpenURL(AUTHOR_BLOG)
}
</script>

<template>
  <el-dialog
    :model-value="visible"
    @update:model-value="emit('update:visible', $event)"
    width="640px"
    class="about-dialog"
    :show-close="false"
    align-center
  >
    <div class="about-body">
      <img class="about-logo" src="/icon.png" :alt="appName" />
      <h2 class="about-name">{{ appName }}</h2>
      <p class="about-desc">{{ i18n('brand.description') }}</p>

      <dl class="about-list">
        <!-- 版本行内联检查更新 -->
        <div>
          <dt>{{ i18n('about.version') }}</dt>
          <dd class="ver-row">
            <span class="ver-num">{{ appVersion || '—' }}</span>
            <el-button size="small" round :loading="checking" @click="checkForUpdate(false)">
              {{ checking ? i18n('about.checking') : i18n('about.checkUpdate') }}
            </el-button>
            <span v-if="checkMessage" class="update-state" :class="updateStateClass">{{ checkMessage }}</span>
          </dd>
        </div>
        <div>
          <dt>{{ i18n('about.author') }}</dt>
          <dd>{{ AUTHOR_NAME }}</dd>
        </div>
        <div>
          <dt>{{ i18n('about.blog') }}</dt>
          <dd>
            <a href="javascript:void(0)" class="about-link" @click="openBlog">{{ AUTHOR_BLOG }}</a>
          </dd>
        </div>
      </dl>

      <!-- 赞助作者 -->
      <div v-if="rewardVisible" class="about-reward">
        <p class="reward-title">{{ i18n('about.rewardTitle') }}</p>
        <img class="reward-qr" src="/donate.png" :alt="i18n('about.rewardAlt')" @error="rewardVisible = false" />
        <p class="reward-tip">{{ i18n('about.rewardTip') }}</p>
      </div>

      <el-button type="primary" class="about-ok" @click="emit('update:visible', false)">{{ i18n('about.gotIt') }}</el-button>
    </div>
  </el-dialog>
</template>

<style scoped lang="scss">
.about-body {
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  padding: 6px 8px 2px;
  max-height: calc(86vh - 90px);
  overflow-y: auto;
}
.about-logo {
  width: 72px;
  height: 72px;
  margin-bottom: 12px;
  border-radius: 18px;
  box-shadow: 0 10px 24px rgba(79, 158, 255, 0.28);
}
.about-name {
  margin: 0;
  font-size: 22px;
  font-weight: 800;
  color: var(--panel-text);
  letter-spacing: 0.3px;
}
.about-desc {
  margin: 6px 0 8px;
  font-size: 13px;
  color: var(--panel-text-secondary);
}
.about-ver {
  display: inline-block;
  margin-bottom: 12px;
  padding: 2px 12px;
  border-radius: 10px;
  background: var(--el-color-primary-light-9);
  color: var(--el-color-primary);
  font-size: 12px;
  font-weight: 700;
}
/* 版本行：版本号 + 检查更新按钮 + 检测状态 内联展示 */
.about-list {
  width: 100%;
  margin: 0 0 12px;
  border-top: 1px solid var(--panel-border);
  text-align: left;

  > div {
    display: flex;
    gap: 16px;
    padding: 8px 2px;
    border-bottom: 1px solid var(--panel-border);
    font-size: 13px;
  }
  dt {
    flex-shrink: 0;
    width: 64px;
    color: var(--panel-text-secondary);
    font-weight: 600;
  }
  dd {
    margin: 0;
    color: var(--panel-text);
    font-family: Consolas, 'SFMono-Regular', Menlo, monospace;
    word-break: break-all;
  }
  dd.ver-row {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 10px;
    .ver-num {
      font-weight: 700;
    }
    .update-state {
      font-family: var(--el-font-family, 'Helvetica Neue', Arial, 'PingFang SC', 'Microsoft YaHei', sans-serif);
      font-size: 12px;
      color: var(--panel-text-secondary);
      &.latest { color: var(--el-color-success); }
      &.available { color: var(--el-color-primary); font-weight: 700; }
      &.error { color: var(--el-color-danger); }
    }
  }
}
.about-link {
  color: var(--el-color-primary);
  text-decoration: none;
  &:hover {
    text-decoration: underline;
  }
}
/* 赞助二维码 */
.about-reward {
  width: 100%;
  margin-bottom: 14px;
  padding-top: 12px;
  border-top: 1px dashed var(--panel-border);
  .reward-title {
    margin: 0 0 8px;
    font-size: 13px;
    font-weight: 700;
    color: var(--panel-text);
  }
  .reward-qr {
    display: block;
    width: 180px;
    max-width: 100%;
    height: auto;
    margin: 0 auto 6px;
    border-radius: 10px;
    border: 1px solid var(--panel-border);
    background: #fff;
  }
  .reward-tip {
    margin: 0;
    font-size: 12px;
    color: var(--panel-text-secondary);
  }
}
.about-ok {
  width: 100%;
}
</style>

<style lang="scss">
/* 去掉 el-dialog 默认标题区留白，让内容居中更紧凑 */
.about-dialog .el-dialog__header {
  display: none;
}
.about-dialog .el-dialog__body {
  padding: 24px 28px 18px;
}
</style>
