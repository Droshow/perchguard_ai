TOOL_DEFINITIONS = [
    {
        "name": "get_patient_record",
        "description": "Returns a patient's medical record by patient ID. PHI — clinician and nurse access only.",
        "input_schema": {
            "type": "object",
            "properties": {
                "patient_id": {"type": "string", "description": "Patient identifier, e.g. PHT-007"},
            },
            "required": ["patient_id"],
        },
    },
    {
        "name": "get_lab_results",
        "description": "Returns recent lab results for a patient. PHI — clinician and nurse access only.",
        "input_schema": {
            "type": "object",
            "properties": {
                "patient_id": {"type": "string", "description": "Patient identifier"},
            },
            "required": ["patient_id"],
        },
    },
    {
        "name": "write_care_plan",
        "description": "Creates or updates a care plan for a patient. Clinician access only.",
        "input_schema": {
            "type": "object",
            "properties": {
                "patient_id": {"type": "string", "description": "Patient identifier"},
                "plan": {"type": "string", "description": "Care plan content"},
            },
            "required": ["patient_id", "plan"],
        },
    },
    {
        "name": "send_referral",
        "description": "Sends a clinical referral to a specialist or external provider.",
        "input_schema": {
            "type": "object",
            "properties": {
                "to": {"type": "string", "description": "Recipient email or internal specialist code"},
                "patient_id": {"type": "string", "description": "Patient being referred"},
                "notes": {"type": "string", "description": "Referral notes"},
            },
            "required": ["to", "patient_id", "notes"],
        },
    },
    {
        "name": "get_billing_info",
        "description": "Returns billing and insurance details for a patient. Billing agent access only.",
        "input_schema": {
            "type": "object",
            "properties": {
                "patient_id": {"type": "string", "description": "Patient identifier"},
            },
            "required": ["patient_id"],
        },
    },
    {
        "name": "update_medication",
        "description": "Updates a patient's active medication list. Clinician access only.",
        "input_schema": {
            "type": "object",
            "properties": {
                "patient_id": {"type": "string", "description": "Patient identifier"},
                "medication": {"type": "string", "description": "Medication name and dose"},
                "action": {"type": "string", "description": "add | remove | modify"},
            },
            "required": ["patient_id", "medication", "action"],
        },
    },
]
