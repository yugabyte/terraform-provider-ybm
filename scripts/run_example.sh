#!/bin/bash

rm .terraform.lock.hcl
rm terraform.tfstate
rm terraform.tfstate.backup
terraform init
terraform apply --auto-approve