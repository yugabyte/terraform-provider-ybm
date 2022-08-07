#!/bin/bash
cd ..
make install
cd samples
rm .terraform.lock.hcl
terraform init
