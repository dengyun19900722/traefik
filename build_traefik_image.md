### 1. visit the package platform

https://www.katacoda.com/courses/kubernetes/playground

### 2. fetch the project

mkdir -p $GOPATH/src/github.com/containous
cd $GOPATH/src/github.com/containous
git clone https://github.com/dengyun19900722/traefik.git
cd traefik/
git checkout v2.0

### 3. make the binary file

make binary

### 4. build traefik image
docker login  ycdocker9527/ca0987hf
docker build -t ycdocker9527/traefik:2.0.0-co-forward .
docker push ycdocker9527/traefik:2.0.0-co-forward