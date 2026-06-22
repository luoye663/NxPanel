import { Button, Group, NumberInput, Stack, Switch, TagsInput, TextInput } from '@mantine/core'
import type { UseFormReturnType } from '@mantine/form'
import { generateLoginPathSuggestion } from '@/api/gate'
import { SectionCard } from '@/components/common/SectionCard'
import type { SecuritySettingsFormValues } from './types'

interface RuntimeSecuritySettingsProps {
  form: UseFormReturnType<SecuritySettingsFormValues>
  saving: boolean
  onSave: () => void
}

export function RuntimeSecuritySettings({ form, saving, onSave }: RuntimeSecuritySettingsProps) {
  return (
    <SectionCard title="运行时安全" description="登录限流、会话绑定和可信代理会保存到后端配置，并由服务热更新；计数窗口格式如 15m、1h。">
      <Stack gap="sm">
        <Group align="flex-end">
          <TextInput
            flex={1}
            label="登录路径"
            description="固定 /login 不再公开；修改后会立即切换 API 入口并重新加载页面。"
            placeholder="/nx-xxxxxxxxxxxxx"
            {...form.getInputProps('login_path')}
          />
          <Button variant="light" onClick={() => form.setFieldValue('login_path', generateLoginPathSuggestion())}>随机生成</Button>
        </Group>
        <Group grow align="flex-start">
          <NumberInput label="最大失败次数" min={1} max={100} {...form.getInputProps('rate_limit_max_failures')} />
          <TextInput label="计数窗口" placeholder="15m" {...form.getInputProps('rate_limit_window')} />
          <NumberInput label="最大并发会话" min={1} max={100} {...form.getInputProps('max_sessions')} />
        </Group>
        <Group align="flex-start">
          <Switch label="绑定会话 IP" description="IP 变化后需重新登录。" {...form.getInputProps('bind_session_ip', { type: 'checkbox' })} />
          <Switch label="绑定 User-Agent" description="浏览器环境变化后需重新登录。" {...form.getInputProps('bind_session_ua', { type: 'checkbox' })} />
        </Group>
        <TagsInput
          label="可信代理"
          placeholder="输入 IP 或 CIDR 后回车"
          description="只有来自这些代理的 X-Forwarded-For / X-Forwarded-Proto 会被信任；留空表示不信任转发头。"
          value={form.values.trusted_proxies}
          onChange={(value) => form.setFieldValue('trusted_proxies', Array.from(new Set(value.map((item) => item.trim()).filter(Boolean))))}
        />
        <Group justify="flex-start"><Button loading={saving} onClick={onSave}>保存安全配置</Button></Group>
      </Stack>
    </SectionCard>
  )
}
