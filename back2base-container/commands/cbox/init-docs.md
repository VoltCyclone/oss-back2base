# Initialize Distributed CLAUDE.md Documentation

**Purpose:** Create or update CLAUDE.md files in project subdirectories to establish distributed documentation for better context management.

**Usage:** `/cbox:init-docs [directory]`

When invoked, you should:

1. **Scan the target directory** (current directory if not specified) for subdirectories
2. **For each subdirectory**, check if it has a CLAUDE.md file
3. **Create or update CLAUDE.md** with:
   - Purpose and responsibility of that module/directory
   - Key files and their functions
   - Important patterns and conventions used
   - Dependencies and interactions with other modules
   - Critical knowledge specific to that area

## Documentation Template

```markdown
# CLAUDE.md - [Directory Name]

## Purpose
Brief description of what this directory/module is responsible for.

## Key Components
- **file1.ext**: Description of its role
- **file2.ext**: Description of its role

## Important Patterns
- Pattern 1: Description
- Pattern 2: Description

## Dependencies
- External dependencies this module relies on
- Other modules it interacts with

## Critical Knowledge
- Important implementation details
- Common pitfalls to avoid
- Performance considerations
- Security considerations

## Recent Changes
- Track significant changes when updating
```

## Best Practices

1. **Be Concise**: Focus on critical knowledge that saves time
2. **Module-Specific**: Document only what's relevant to this directory
3. **Update Regularly**: When making significant changes, update the CLAUDE.md
4. **Cross-Reference**: Reference parent or sibling CLAUDE.md files when relevant
5. **Actionable**: Include information that helps with actual tasks

## Example Workflow

When starting work in tmux mode:
1. Run `/cbox:init-docs` to scan and create missing CLAUDE.md files
2. Review existing CLAUDE.md files in your working directories
3. Update them as you learn critical information
4. This distributed approach keeps context usage efficient