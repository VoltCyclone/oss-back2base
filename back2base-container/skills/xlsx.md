---
name: xlsx
model: sonnet
context: fork
agent: general-purpose
paths: "**/*.xlsx,**/*.xlsm,**/*.csv,**/*.tsv"
description: "Use this skill any time a spreadsheet file is the primary input or output. This means any task where the user wants to: open, read, edit, or fix an existing .xlsx, .xlsm, .csv, or .tsv file; create a new spreadsheet from scratch or from other data sources; or convert between tabular file formats."
---

# XLSX creation, editing, and analysis

## CRITICAL: Use Formulas, Not Hardcoded Values

Always use Excel formulas instead of calculating values in Python and hardcoding them.

```python
# Good: Let Excel calculate
sheet['B10'] = '=SUM(B2:B9)'
sheet['C5'] = '=(C4-C2)/C2'
sheet['D20'] = '=AVERAGE(D2:D19)'
```

## Reading and analyzing data

```python
import pandas as pd

df = pd.read_excel('file.xlsx')
all_sheets = pd.read_excel('file.xlsx', sheet_name=None)

df.head()
df.info()
df.describe()

df.to_excel('output.xlsx', index=False)
```

## Creating new Excel files

```python
from openpyxl import Workbook
from openpyxl.styles import Font, PatternFill, Alignment

wb = Workbook()
sheet = wb.active

sheet['A1'] = 'Hello'
sheet['B1'] = 'World'
sheet.append(['Row', 'of', 'data'])

sheet['B2'] = '=SUM(A1:A10)'

sheet['A1'].font = Font(bold=True, color='FF0000')
sheet['A1'].fill = PatternFill('solid', start_color='FFFF00')
sheet['A1'].alignment = Alignment(horizontal='center')

sheet.column_dimensions['A'].width = 20

wb.save('output.xlsx')
```

## Editing existing Excel files

```python
from openpyxl import load_workbook

wb = load_workbook('existing.xlsx')
sheet = wb.active

sheet['A1'] = 'New Value'
sheet.insert_rows(2)
sheet.delete_cols(3)

new_sheet = wb.create_sheet('NewSheet')
new_sheet['A1'] = 'Data'

wb.save('modified.xlsx')
```

## Financial Models - Color Coding Standards

- **Blue text (0,0,255)**: Hardcoded inputs
- **Black text (0,0,0)**: ALL formulas and calculations
- **Green text (0,128,0)**: Links from other worksheets
- **Red text (255,0,0)**: External links
- **Yellow background (255,255,0)**: Key assumptions

## Number Formatting Standards

- **Years**: Format as text strings ("2024" not "2,024")
- **Currency**: $#,##0 format; specify units in headers
- **Zeros**: Format as "-"
- **Percentages**: 0.0% (one decimal)
- **Multiples**: 0.0x for valuation multiples
- **Negative numbers**: Use parentheses (123) not minus -123

## Best Practices

- **pandas**: Best for data analysis, bulk operations
- **openpyxl**: Best for complex formatting, formulas, Excel-specific features
- Cell indices are 1-based
- Use `data_only=True` to read calculated values (warning: saves will replace formulas with values)
- For large files: `read_only=True` or `write_only=True`
