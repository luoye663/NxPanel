import { createTheme } from '@mantine/core'

export const theme = createTheme({
  primaryColor: 'blue',
  defaultRadius: 'md',
  fontFamily: 'ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
  fontFamilyMonospace: '"JetBrains Mono", "SFMono-Regular", Consolas, "Liberation Mono", monospace',
  headings: {
    fontWeight: '600',
  },
  components: {
    ActionIcon: { defaultProps: { radius: 'md', size: 'sm' } },
    Badge: { defaultProps: { size: 'sm' } },
    Button: { defaultProps: { radius: 'md', size: 'sm' } },
    Card: { defaultProps: { withBorder: true, radius: 'md' } },
    Checkbox: { defaultProps: { size: 'sm' } },
    Grid: { defaultProps: { gutter: 'sm' } },
    Group: { defaultProps: { gap: 'sm' } },
    Modal: { defaultProps: { centered: true } },
    NativeSelect: { defaultProps: { size: 'sm' } },
    NumberInput: { defaultProps: { size: 'sm' } },
    PasswordInput: { defaultProps: { size: 'sm' } },
    Select: { defaultProps: { size: 'sm' } },
    SegmentedControl: { defaultProps: { size: 'sm' } },
    Stack: { defaultProps: { gap: 'sm' } },
    Switch: { defaultProps: { size: 'sm' } },
    Table: { defaultProps: { highlightOnHover: true, verticalSpacing: 'sm' } },
    TagsInput: { defaultProps: { size: 'sm' } },
    Text: { defaultProps: { size: 'sm' } },
    TextInput: { defaultProps: { size: 'sm' } },
  },
})
