#!/bin/bash
set -eo pipefail

# $1 splitter
# $2 globPatterns
function multiGlobToRegex() {
    splitter="$1"
    globPatterns="$2"
    next=false
    result=""
    for globPattern in $(echo "$globPatterns" | tr $splitter "\n" ); do
        if $next; then
            result="$result|"
        fi
        result="$result($(globToRegex "$globPattern"))"
        next=true
    done
    echo "$result"
}

# $1 glob pattern
function globToRegex() {
    globPattern="$1"
    result=""
    for (( i=0; i<${#globPattern}; i++ )); do
        c="${globPattern:$i:1}"
        case $c in
            '?')
                result="${result}."
                ;;
            '*')
                result="${result}.*"
                ;;
            '.' | '+' | '(' | ')' | '[' | ']' | '{' | '}' | '^' | '$' | '|' | '\\' | ',' | '-' | '!' | '@' | '%' | '&' | '#' | '~' | '`' | ';' | ':' | '=' | '<' | '>' | ' ')
                result="${result}\\$c"
                ;;
            *)
                result="${result}$c"
                ;;

        esac
    done
    echo $result
}
