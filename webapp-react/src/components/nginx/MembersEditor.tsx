import { ActionIcon, Box, Checkbox, Group, NumberInput, Stack, Text, TextInput, Tooltip } from '@mantine/core'
import { IconArrowDown, IconArrowUp, IconCopy, IconPlus, IconTrash } from '@tabler/icons-react'
import type { NginxUpstreamServerRequest } from '@/api/types'

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
        <Text size="sm" fw={500}>成员</Text>
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
              label="地址"
              placeholder="127.0.0.1:8080"
              value={member.address}
              error={errors[index] || undefined}
              onChange={(event) => update(index, { address: event.currentTarget.value })}
            />
            <NumberInput label="权重" min={1} max={256} value={member.weight} onChange={(value) => update(index, { weight: Number(value) || 1 })} />
            <NumberInput label="失败次数" min={0} max={100} value={member.max_fails} onChange={(value) => update(index, { max_fails: Number(value) || 0 })} />
            <NumberInput label="失败超时（秒）" min={1} max={3600} value={member.fail_timeout_seconds} onChange={(value) => update(index, { fail_timeout_seconds: Number(value) || 1 })} />
            <NumberInput label="排序值" min={-100000} max={100000} value={member.sort_order} onChange={(value) => update(index, { sort_order: Number(value) || 0 })} />
          </div>
          <Group mt="sm" justify="space-between" wrap="wrap">
            <Group gap="md">
              <Checkbox label="备用" checked={member.backup} onChange={(event) => update(index, { backup: event.currentTarget.checked, down: event.currentTarget.checked ? false : member.down })} />
              <Checkbox label="停用" checked={member.down} onChange={(event) => update(index, { down: event.currentTarget.checked, backup: event.currentTarget.checked ? false : member.backup })} />
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
