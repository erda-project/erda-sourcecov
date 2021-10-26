#!/bin/bash

set -eo pipefail

cd "$(dirname "$0")"
source ./glob.sh

function pushd () {
    command pushd "$@" > /dev/null
}

function popd () {
    command popd "$@" > /dev/null
}

_jarPathes=$1
destPath=$2
includes=${INCLUDES}
excludes=${EXCLUDES}
groupIdPrefixBlacklist=(
  "net.bytebuddy"
  "org.apache"
  "org.glassfish"
  "com.fasterxml"
  "io.netty"
  "org.springframework"
  "io.github"
  "com.google"
  "com.alibaba"
  "javax"
  "org.jboss"
  "com.aliyun"
  "commons-"
  "com.sun"
  "org.yaml"
  "jakarta"
  "net.sf"
  "com.github"
  "com.codehaus"
  "org.jacoco"
  "software.amazon"
  "redis"
  "org.slf4j"
  "org.redis"
  "org.hibernate"
  "org.ehcache"
  "com.amazon"
  "cn.hutool"
  "org.quartz"
)

# check args right
[ -z "$_jarPathes" ] && { echo "Not given jarPath"; exit 1; }
[ -z "$destPath" ] && { echo "Not given destPath"; exit 1; }

if [[ $includes == '*' ]]; then
    includes=''
fi
if [[ $excludes == '*' ]]; then
    excludes=''
fi

includes=$(multiGlobToRegex ':' "$includes")
excludes=$(multiGlobToRegex ':' "$excludes")

if [[ -z "$includes" ]]; then
    includes='.*'
fi

echo "includes: $includes"
echo "excludes: $excludes"

IFS=',' read -ra jars <<< "$_jarPathes"
for jarPath in "${jars[@]}"; do
  echo "input jarPath: $jarPath"
  # check jarPath exist
  [ -f "$jarPath" ] || { echo "jarPath $jarPath not exists!"; exit 1; }
done

# check dest path exist or create
if [[ ! -d "$destPath" ]]; then
  echo "$destPath not exist, tring to create..."
  mkdir -p "$destPath"
fi

destSubPath=$destPath/sub
rm -rf $destSubPath
mkdir -p $destSubPath


mkdir -p $destSubPath/fatjar
mkdir -p $destSubPath/libjarex
mkdir -p $destSubPath/libjarsrc
mkdir -p $destSubPath/libjarcls


extra_one_jar() {
    jarPath=$(realpath $1)

    #fatjarFile=${jarPath##*/}
    fatjarFile=$(echo $jarPath | md5sum - | cut -d' ' -f1)
    fatjarExPath=$destSubPath/fatjar/${fatjarFile}

    mkdir $fatjarExPath
    pushd $fatjarExPath
    unzip -o -q $jarPath >/dev/null 2>&1 || true
    popd

    fatjarLibPath=$fatjarExPath/BOOT-INF/lib/

    # judge
    if [[ ! -d "${fatjarExPath}/BOOT-INF" ]]; then
        echo "jarPath $jarPath is not a fatjar"
        return
    fi

    # copy app own classes
    main_cls_dir="$destSubPath/libjarcls/main/"
    mkdir -p $main_cls_dir
    cp -a $fatjarExPath/BOOT-INF/classes/. $main_cls_dir

    fatjarPom=$(ls $fatjarExPath/META-INF/maven/*/*/pom.xml | head -n 1)
    [ -f "$fatjarPom" ] || { echo "fatjarPom not exists!"; exit 1; }
    fatjarPomDir=$(dirname $fatjarPom)

    pushd $fatjarPomDir
    echo "begin copy-dependencies-sources from pom.xml"
    mvn dependency:copy-dependencies -Dclassifier=sources &> dep.log || echo "download dep fail, log: $fatjarPomDir/dep.log"
    echo "end copy-dependencies-sources"
    popd


    for i in `find $fatjarLibPath -name '*.jar'`; do
        libjarFile=${i##*/}
        libjarExPath=$destSubPath/libjarex/${libjarFile}.d
        unzip -o -q -d $libjarExPath $i || echo "unzip to libjarExPath fail: $i"
        mmPath=$libjarExPath/META-INF/maven
        if [[ -d $mmPath ]]; then
            propFile=$(find $mmPath -name 'pom.properties' -print -quit)
            if [[ ! -f "$propFile" ]]; then
                echo "pom.properties not exists: $propFile, continue"
                continue
            fi
            groupId=$(cat $propFile | grep groupId | cut -d= -f2)
            artifactId=$(cat $propFile | grep artifactId | cut -d= -f2)
            version=$(cat $propFile | grep version | cut -d= -f2)
            if [[ ! -z "$groupId" && ! -z "$artifactId" && ! -z "$version" ]]; then
                in_black=false
                for black in "${groupIdPrefixBlacklist[@]}"; do
                    if [[ "$groupId" == "$black"* ]]; then
                        in_black=true
                        break
                    fi
                done
                if ! $in_black; then
                    echo "handling fatjarlib: $groupId:$artifactId:$version ..."
                    unzip -o -q -d $destSubPath/libjarcls $i -x "META-INF/*" || echo "unzip fatjarlib fail: $i"
                    libjarsrcFile=${libjarFile%.jar}-sources.jar
                    libjarsrcPath=$fatjarPomDir/target/dependency/$libjarsrcFile
                    if [[ -f "$libjarsrcPath" ]]; then
                        unzip -o -q -d $destSubPath/libjarsrc/ $libjarsrcPath -x "META-INF/*" || echo "unzip fatjarlib-src fail: $libjarsrcPath"
                    fi
                fi
            fi
        fi
    done

}

for jarPath in "${jars[@]}"; do
    echo "You want extract jar from $jarPath to $destPath, right?"
    extra_one_jar $jarPath
    echo "Finish handle jar: $jarPath"
done

# filter cls & src by includes & excludes
## cls
pushd $destSubPath/libjarcls
echo "deleting non class files firstly..."
find . -type f ! -name "*.class" -delete
echo "deleted"
class_dirs=$(find . -name "*.class" -printf '%h\n' | sort -u)
# 处理每个目录，若不符合，则把文件全部删除，不删除目录
echo "filter classes by includes & excludes ..."
for dir in ${class_dirs}; do
    # remove prefix ./
    dir_for_grep="${dir#./}"
    # handle app main class
    dir_for_grep="${dir_for_grep#main/}"
    if ! echo $dir_for_grep | grep -Ex "$includes" | grep -q -v -Ex "$excludes"; then
        # not matched, only delete all files in this dir
        echo "delete excluded classes under dir: $dir"
        find $dir -maxdepth 1 -type f -name "*.class" -delete
    fi
done
# 删除空目录
find . -type d -empty -delete
# 应用代码 class
if [[ -d main ]]; then
    cp -r main/* .
    rm -fr main
fi
popd


# delete class is enough
## src
pushd $destSubPath/libjarsrc
echo "deleting non source files firstly..."
find . -type f ! -name "*.java" -delete
echo "deleted"
source_dirs=$(find . -name "*.java" -printf '%h\n' | sort -u)
# 处理每个目录，若不符合，则把文件全部删除，不删除目录
echo "filter sources by includes & excludes ..."
for dir in ${source_dirs}; do
    # remove prefix ./
    dir_for_grep="${dir#./}"
    # handle app main source
    dir_for_grep="${dir_for_grep#main/}"
    if ! echo $dir_for_grep | grep -Ex "$includes" | grep -q -v -Ex "$excludes"; then
        # not matched, only delete all files in this dir
        echo "delete excluded sources under dir: $dir"
        find $dir -maxdepth 1 -type f -name "*.java" -delete
    fi
done
# 删除空目录
find . -type d -empty -delete
# 应用代码 class
if [[ -d main ]]; then
    cp -r main/* .
    rm -fr main
fi
popd


echo "Finish!"
