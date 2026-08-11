import { ActionIcon, Box, Checkbox, Group, NumberInput, Stack, TextInput, Tooltip } from '@mantine/core'
import { IconArrowDown, IconArrowUp, IconCopy, IconPlus, IconTrash } from '@tabler/icons-react'
import type { NginxUpstreamServerRequest } from '@/api/types'
import { UpstreamHelpLabel } from './UpstreamHelpLabel'

interface MembersEditorProps {
  members: NginxUpstreamServerRequest[]
  errors: string[]
  onChange: (members: NginxUpstreamServerRequest[]) => void
}

export const emptyUpstreamMember = (): NginxUpstreamServerRequest => ({
  address: '',
  weight: 1,
  max_fails: 1,
  fail_timeout_seconds: 10,
  backup: false,
  down: false,
  sort_order: 0,
})

export function MembersEditor({ members, errors, onChange }: MembersEditorProps) {
  const atLimit = members.length >= 128
  function update(index: number, patch: Partial<NginxUpstreamServerRequest>) {
    onChange(members.map((member, memberIndex) => memberIndex === index ? { ...member, ...patch } : member))
  }

  function move(index: number, offset: number) {
    const nextIndex = index + offset
    if (nextIndex < 0 || nextIndex >= members.length) return
    const next = [...members]
    const [member] = next.splice(index, 1)
    next.splice(nextIndex, 0, member)
    onChange(next.map((item, sortOrder) => ({ ...item, sort_order: sortOrder })))
  }

  return (
    <Stack gap="sm">
      <Group justify="space-between">
        <UpstreamHelpLabel label="成员" help="请求会按负载均衡策略分配到未停用的成员。最多可配置 128 个成员。" />
        <Tooltip label="添加成员">
          <ActionIcon variant="light" disabled={atLimit} onClick={() => onChange([...members, { ...emptyUpstreamMember(), sort_order: members.length }])} aria-label={atLimit ? '成员数量已达到 128 个上限' : '添加成员'}>
            <IconPlus size={17} />
          </ActionIcon>
        </Tooltip>
      </Group>
      {members.map((member, index) => (
        <Box key={`${index}-${member.sort_order}`} className="upstreamMemberBlock">
          <div className="upstreamMemberGrid">
            <TextInput
              className="upstreamMemberAddress"
              label={<UpstreamHelpLabel label="地址" help="后端监听地址，支持 hostname:port、IPv4:port 或 [IPv6]:port，不支持 URL 路径。" />}
              placeholder="127.0.0.1:8080"
              value={member.address}
              error={errors[index] || undefined}
              onChange={(event) => update(index, { address: event.currentTarget.value })}
            />
            <NumberInput label={<UpstreamHelpLabel label="权重" help="成员获得请求的相对比例。权重越高，通常分配到的请求越多。" />} min={1} max={256} value={member.weight} onChange={(value) => update(index, { weight: Number(value) || 1 })} />
            <NumberInput label={<UpstreamHelpLabel label="失败次数" help="在失败超时时间内达到该失败次数后，Nginx 会暂时将成员视为不可用。0 表示关闭失败计数。" />} min={0} max={100} value={member.max_fails} onChange={(value) => update(index, { max_fails: Number(value) || 0 })} />
            <NumberInput label={<UpstreamHelpLabel label="失败超时" help="单位（秒）统计失败次数的时间窗口，也是成员被判定不可用后的暂停时长。" />} min={1} max={3600} value={member.fail_timeout_seconds} onChange={(value) => update(index, { fail_timeout_seconds: Number(value) || 1 })} />
            <NumberInput label={<UpstreamHelpLabel label="排序值" help="控制成员在生成配置中的顺序，数值较小的成员排在前面；拖动上移或下移会自动重排。" />} min={-100000} max={100000} value={member.sort_order} onChange={(value) => update(index, { sort_order: Number(value) || 0 })} />
          </div>
          <Group mt="sm" justify="space-between" wrap="wrap">
            <Group gap="md">
              <Checkbox label={<UpstreamHelpLabel label="备用" help="仅当所有主成员不可用时接收请求。IP Hash 和自定义 Hash 策略不支持备用成员。" />} checked={member.backup} onChange={(event) => update(index, { backup: event.currentTarget.checked, down: event.currentTarget.checked ? false : member.down })} />
              <Checkbox label={<UpstreamHelpLabel label="停用" help="保留成员配置但不向其分配请求。停用成员不能同时设为备用。" />} checked={member.down} onChange={(event) => update(index, { down: event.currentTarget.checked, backup: event.currentTarget.checked ? false : member.backup })} />
            </Group>
            <Group gap={4} wrap="nowrap">
               <Tooltip label="上移"><ActionIcon variant="subtle" aria-label={`上移成员 ${index + 1}`} disabled={index === 0} onClick={() => move(index, -1)}><IconArrowUp size={16} /></ActionIcon></Tooltip>
               <Tooltip label="下移"><ActionIcon variant="subtle" aria-label={`下移成员 ${index + 1}`} disabled={index === members.length - 1} onClick={() => move(index, 1)}><IconArrowDown size={16} /></ActionIcon></Tooltip>
               <Tooltip label={atLimit ? '成员数量已达到上限' : '复制'}><ActionIcon variant="subtle" aria-label={`复制成员 ${index + 1}`} disabled={atLimit} onClick={() => onChange([...members.slice(0, index + 1), { ...member, address: '', sort_order: index + 1 }, ...members.slice(index + 1)].map((item, sortOrder) => ({ ...item, sort_order: sortOrder })))}><IconCopy size={16} /></ActionIcon></Tooltip>
               <Tooltip label={members.length === 1 ? '至少保留一个成员' : '删除'}><ActionIcon color="red" variant="subtle" aria-label={`删除成员 ${index + 1}`} disabled={members.length === 1} onClick={() => onChange(members.filter((_, memberIndex) => memberIndex !== index).map((item, sortOrder) => ({ ...item, sort_order: sortOrder })))}><IconTrash size={16} /></ActionIcon></Tooltip>
            </Group>
          </Group>
        </Box>
      ))}
    </Stack>
  )
}
