#!/bin/bash

# Kubernetes Resource Management Script
# Usage: ./script.sh [create|mutate|delete|cleanup] [options]

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default namespace
NAMESPACE="default"
RESOURCE_DIR="./k8s-resources"
BACKUP_DIR="./backups"

# Function to print colored output
print_status() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

print_header() {
    echo -e "${BLUE}=== $1 ===${NC}"
}

# Function to create directories
setup_directories() {
    mkdir -p "$RESOURCE_DIR"
    mkdir -p "$BACKUP_DIR"
}

# Function to create a ConfigMap
create_configmap() {
    local name=$1
    local data_file=$2
    
    cat <<EOF > "$RESOURCE_DIR/configmap-$name.yaml"
apiVersion: v1
kind: ConfigMap
metadata:
  name: $name
  namespace: $NAMESPACE
data:
EOF

    if [[ -f "$data_file" ]]; then
        while IFS='=' read -r key value; do
            [[ $key =~ ^#.*$ ]] && continue
            [[ -z "$key" ]] && continue
            echo "  $key: \"$value\"" >> "$RESOURCE_DIR/configmap-$name.yaml"
        done < "$data_file"
    else
        echo "  example-key: \"example-value\"" >> "$RESOURCE_DIR/configmap-$name.yaml"
    fi
    
    kubectl apply -f "$RESOURCE_DIR/configmap-$name.yaml"
    print_status "Created ConfigMap: $name"
}

# Function to create a Service
create_service() {
    local name=$1
    local port=${2:-80}
    local target_port=${3:-8080}
    local selector_key=${4:-"app"}
    local selector_value=${5:-"$name"}
    
    cat <<EOF > "$RESOURCE_DIR/service-$name.yaml"
apiVersion: v1
kind: Service
metadata:
  name: $name
  namespace: $NAMESPACE
spec:
  selector:
    $selector_key: $selector_value
  ports:
  - protocol: TCP
    port: $port
    targetPort: $target_port
  type: ClusterIP
EOF
    
    kubectl apply -f "$RESOURCE_DIR/service-$name.yaml"
    print_status "Created Service: $name"
}

# Function to create a Pod
create_pod() {
    local name=$1
    local image=${2:-"nginx:alpine"}
    local configmap_name=${3:-""}
    
    cat <<EOF > "$RESOURCE_DIR/pod-$name.yaml"
apiVersion: v1
kind: Pod
metadata:
  name: $name
  namespace: $NAMESPACE
  labels:
    app: $name
spec:
  containers:
  - name: $name
    image: $image
    ports:
    - containerPort: 8080
EOF

    if [[ -n "$configmap_name" ]]; then
        cat <<EOF >> "$RESOURCE_DIR/pod-$name.yaml"
    volumeMounts:
    - name: config-volume
      mountPath: /etc/config
  volumes:
  - name: config-volume
    configMap:
      name: $configmap_name
EOF
    fi
    
    kubectl apply -f "$RESOURCE_DIR/pod-$name.yaml"
    print_status "Created Pod: $name"
}

# Function to label default storage class
label_storage_class() {
    local storage_class_name=$(kubectl get storageclass -o jsonpath='{.items[?(@.metadata.annotations.storageclass\.kubernetes\.io/is-default-class=="true")].metadata.name}')
    
    if [[ -z "$storage_class_name" ]]; then
        print_warning "No default storage class found"
        return 1
    fi
    
    kubectl label storageclass "$storage_class_name" \
        managed-by=script \
        created-by=resource-manager \
        --overwrite
    
    print_status "Labeled storage class: $storage_class_name"
}

# Function to create resources in batch
create_resources_batch() {
    print_header "Creating Resources in Batch"
    
    # Create ConfigMaps
    create_configmap "app-config" "./config/app.properties"
    create_configmap "db-config" "./config/database.properties"
    
    # Create Services
    create_service "web-service" 80 8080 "app" "web-app"
    create_service "api-service" 8080 3000 "app" "api-app"
    create_service "db-service" 5432 5432 "app" "database"
    
    # Create Pods
    create_pod "web-app" "nginx:alpine" "app-config"
    create_pod "api-app" "node:16-alpine" "app-config"
    create_pod "database" "postgres:13-alpine" "db-config"
    
    # Label storage class
    label_storage_class
    
    print_status "Batch resource creation completed"
}

# Function to mutate resources in a loop
mutate_resources() {
    print_header "Mutating Resources"
    
    local resources=("pods" "services" "configmaps")
    local iterations=${1:-3}
    
    for ((i=1; i<=iterations; i++)); do
        print_status "Mutation iteration $i/$iterations"
        
        for resource_type in "${resources[@]}"; do
            local resources_list=$(kubectl get "$resource_type" -n "$NAMESPACE" -o name)
            
            for resource in $resources_list; do
                # Add mutation label
                kubectl label "$resource" -n "$NAMESPACE" \
                    "mutation-iteration=$i" \
                    "mutated-at=$(date +%s)" \
                    --overwrite
                
                print_status "Mutated $resource (iteration $i)"
            done
        done
        
        # Wait between iterations
        if [[ $i -lt $iterations ]]; then
            sleep 2
        fi
    done
    
    print_status "Resource mutation completed"
}

# Function to delete specific resources
delete_resources() {
    local resource_type=$1
    local name_pattern=${2:-""}
    
    if [[ -n "$name_pattern" ]]; then
        kubectl delete "$resource_type" -n "$NAMESPACE" -l "$name_pattern"
        print_status "Deleted $resource_type with pattern: $name_pattern"
    else
        kubectl delete "$resource_type" -n "$NAMESPACE" --all
        print_status "Deleted all $resource_type"
    fi
}

# Function to cleanup all resources created by this script
cleanup_all() {
    print_header "Cleaning Up All Resources"
    
    # Remove labels from storage class
    local storage_class_name=$(kubectl get storageclass -o jsonpath='{.items[?(@.metadata.labels.managed-by=="script")].metadata.name}')
    if [[ -n "$storage_class_name" ]]; then
        kubectl label storageclass "$storage_class_name" managed-by- created-by- --overwrite
        print_status "Removed labels from storage class: $storage_class_name"
    fi
    
    # Delete all resources with script labels
    local resource_types=("pods" "services" "configmaps")
    
    for resource_type in "${resource_types[@]}"; do
        kubectl delete "$resource_type" -n "$NAMESPACE" -l "app" --ignore-not-found=true
        kubectl delete "$resource_type" -n "$NAMESPACE" -l "mutation-iteration" --ignore-not-found=true
        print_status "Cleaned up $resource_type"
    done
    
    # Clean up local files
    if [[ -d "$RESOURCE_DIR" ]]; then
        rm -rf "$RESOURCE_DIR"
        print_status "Removed local resource files"
    fi
    
    print_status "Cleanup completed"
}

# Function to backup resources before operations
backup_resources() {
    local backup_name="backup-$(date +%Y%m%d-%H%M%S)"
    local backup_path="$BACKUP_DIR/$backup_name"
    
    mkdir -p "$backup_path"
    
    kubectl get all -n "$NAMESPACE" -o yaml > "$backup_path/all-resources.yaml"
    kubectl get configmaps -n "$NAMESPACE" -o yaml > "$backup_path/configmaps.yaml"
    kubectl get storageclass -o yaml > "$backup_path/storage-classes.yaml"
    
    print_status "Resources backed up to: $backup_path"
}

# Function to show usage
show_usage() {
    cat <<EOF
Kubernetes Resource Management Script

Usage: $0 [COMMAND] [OPTIONS]

Commands:
  create          Create resources (pods, services, configmaps)
  create-batch    Create multiple resources in batch
  mutate [N]      Mutate existing resources N times (default: 3)
  delete TYPE     Delete specific resource type (pods|services|configmaps)
  cleanup         Clean up all resources created by this script
  backup          Backup current resources before operations
  status          Show status of created resources

Options:
  -n, --namespace NAMESPACE    Use specific namespace (default: default)
  -h, --help                   Show this help message

Examples:
  $0 create-batch              # Create resources in batch
  $0 mutate 5                  # Mutate resources 5 times
  $0 delete pods               # Delete all pods
  $0 cleanup                   # Clean up all resources
  $0 status                    # Show resource status

EOF
}

# Function to show resource status
show_status() {
    print_header "Resource Status"
    
    echo "Namespace: $NAMESPACE"
    echo
    
    echo "Pods:"
    kubectl get pods -n "$NAMESPACE" -o wide
    
    echo
    echo "Services:"
    kubectl get services -n "$NAMESPACE"
    
    echo
    echo "ConfigMaps:"
    kubectl get configmaps -n "$NAMESPACE"
    
    echo
    echo "Storage Classes:"
    kubectl get storageclass
}

# Main script logic
main() {
    setup_directories
    
    case "${1:-}" in
        "create")
            create_resources_batch
            ;;
        "create-batch")
            create_resources_batch
            ;;
        "mutate")
            local iterations=${2:-3}
            mutate_resources "$iterations"
            ;;
        "delete")
            local resource_type=${2:-}
            if [[ -z "$resource_type" ]]; then
                print_error "Resource type required for delete command"
                show_usage
                exit 1
            fi
            delete_resources "$resource_type"
            ;;
        "cleanup")
            cleanup_all
            ;;
        "backup")
            backup_resources
            ;;
        "status")
            show_status
            ;;
        "-h"|"--help"|"help")
            show_usage
            ;;
        "")
            print_error "Command required"
            show_usage
            exit 1
            ;;
        *)
            print_error "Unknown command: $1"
            show_usage
            exit 1
            ;;
    esac
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -n|--namespace)
            NAMESPACE="$2"
            shift 2
            ;;
        *)
            break
            ;;
    esac
done

# Run main function with remaining arguments
main "$@"
