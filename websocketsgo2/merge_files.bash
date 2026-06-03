#!/bin/bash

# Output file
output_file="websockets.go-percy.md"

# Clear the output file if it already exists
> "$output_file"

# Loop through all .html files in subdirectories
find . -type f -name "*.go" | while read file; do
    # Get the parent folder name
    folder_name=$(basename "$(dirname "$file")")
    
    # Get the actual file name
    file_name=$(basename "$file")

    # Add Markdown header with the folder name and actual file name
    echo "### filename: $folder_name/$file_name, starts" >> "$output_file"
    
    # Start a Markdown code block for go
    echo '```go' >> "$output_file"
    
    # Append the content of the file
    cat "$file" >> "$output_file"
    
    # End the Markdown code block
    echo -e '```\n' >> "$output_file"
    
    # Add Markdown footer
    echo "### filename: $folder_name/$file_name, ends" >> "$output_file"
    echo -e "\n---\n" >> "$output_file"
done

echo "All HTML files have been merged into $output_file with Markdown headers and code blocks."
